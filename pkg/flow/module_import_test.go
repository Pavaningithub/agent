package flow_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/grafana/agent/pkg/flow"
	"github.com/grafana/agent/pkg/flow/internal/testcomponents"
	"github.com/stretchr/testify/require"

	_ "github.com/grafana/agent/component/module/string"
)

func TestImportModule(t *testing.T) {
	testCases := []struct {
		name         string
		module       string
		otherModule  string
		config       string
		updateModule func(filename string) string
		updateFile   string
	}{
		{
			name: "TestImportModule",
			module: `
                declare "test" {
                    argument "input" {
                        optional = false
                    }

                    testcomponents.passthrough "pt" {
                        input = argument.input.value
                        lag = "1ms"
                    }

                    export "output" {
                        value = testcomponents.passthrough.pt.output
                    }
                }`,
			config: `
                testcomponents.count "inc" {
                    frequency = "10ms"
                    max = 10
                }

                import.file "testImport" {
                    filename = "my_module"
                }

                testImport.test "myModule" {
                    input = testcomponents.count.inc.count
                }

                testcomponents.summation "sum" {
                    input = testImport.test.myModule.output
                }
            `,
			updateModule: func(filename string) string {
				return `
                    declare "test" {
                        argument "input" {
                            optional = false
                        }

                        testcomponents.passthrough "pt" {
                            input = argument.input.value
                            lag = "1ms"
                        }

                        export "output" {
                            value = -10
                        }
                    }
                `
			},
			updateFile: "my_module",
		},
		{
			name: "TestImportModuleNoArgs",
			module: `
                declare "test" {
                    testcomponents.passthrough "pt" {
                        input = 10
                        lag = "1ms"
                    }

                    export "output" {
                        value = testcomponents.passthrough.pt.output
                    }
                }`,
			config: `
                import.file "testImport" {
                    filename = "my_module"
                }

                testImport.test "myModule" {
                }

                testcomponents.summation "sum" {
                    input = testImport.test.myModule.output
                }
            `,
			updateModule: func(filename string) string {
				return `
                    declare "test" {
                        testcomponents.passthrough "pt" {
                            input = -10
                            lag = "1ms"
                        }

                        export "output" {
                            value = testcomponents.passthrough.pt.output
                        }
                    }
                `
			},
			updateFile: "my_module",
		},
		{
			name: "TestNestedImportModule",
			module: `
                import.file "otherModule" {
                    filename = "other_module"
                }
            `,
			otherModule: `
                declare "test" {
                    argument "input" {
                        optional = false
                    }

                    testcomponents.passthrough "pt" {
                        input = argument.input.value
                        lag = "1ms"
                    }

                    export "output" {
                        value = testcomponents.passthrough.pt.output
                    }
                }
            `,
			config: `
                testcomponents.count "inc" {
                    frequency = "10ms"
                    max = 10
                }

                import.file "testImport" {
                    filename = "my_module"
                }

                testImport.otherModule.test "myModule" {
                    input = testcomponents.count.inc.count
                }

                testcomponents.summation "sum" {
                    input = testImport.otherModule.test.myModule.output
                }
            `,
			updateModule: func(filename string) string {
				return `
                    declare "test" {
                        argument "input" {
                            optional = false
                        }

                        testcomponents.passthrough "pt" {
                            input = argument.input.value
                            lag = "1ms"
                        }

                        export "output" {
                            value = -10
                        }
                    }
                `
			},
			updateFile: "other_module",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filename := "my_module"
			require.NoError(t, os.WriteFile(filename, []byte(tc.module), 0664))
			defer os.Remove(filename)

			otherFilename := "other_module"
			if tc.otherModule != "" {
				require.NoError(t, os.WriteFile(otherFilename, []byte(tc.otherModule), 0664))
				defer os.Remove(otherFilename)
			}

			// Setup and run controller
			ctrl := flow.New(testOptions(t))
			f, err := flow.ParseSource(t.Name(), []byte(tc.config))
			require.NoError(t, err)
			require.NotNil(t, f)

			err = ctrl.LoadSource(f, nil, nil)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				ctrl.Run(ctx)
				close(done)
			}()
			defer func() {
				cancel()
				<-done
			}()

			// Check for initial condition
			require.Eventually(t, func() bool {
				export := getExport[testcomponents.SummationExports](t, ctrl, "", "testcomponents.summation.sum")
				return export.LastAdded == 10
			}, 3*time.Second, 10*time.Millisecond)

			// Update module if needed
			if tc.updateModule != nil {
				newModule := tc.updateModule(tc.updateFile)
				require.NoError(t, os.WriteFile(tc.updateFile, []byte(newModule), 0664))

				require.Eventually(t, func() bool {
					export := getExport[testcomponents.SummationExports](t, ctrl, "", "testcomponents.summation.sum")
					return export.LastAdded == -10
				}, 3*time.Second, 10*time.Millisecond)
			}
		})
	}
}
