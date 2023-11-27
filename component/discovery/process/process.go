package process

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/agent/component"
	"github.com/grafana/agent/component/discovery"
)

func init() {
	component.Register(component.Registration{
		Name:    "discovery.process",
		Args:    Arguments{},
		Exports: discovery.Exports{},

		Build: func(opts component.Options, args component.Arguments) (component.Component, error) {
			return New(opts, args.(Arguments))
		},
	})
}

type Arguments struct {
	Join            []discovery.Target `river:"join,attr,optional"`
	RefreshInterval time.Duration      `river:"refresh_interval,attr,optional"`
	ProcFS          string             `river:"proc_fs,attr,optional"`
}

var DefaultConfig = Arguments{
	Join:            nil,
	RefreshInterval: 14 * time.Second,
	ProcFS:          "/proc",
}

func (args *Arguments) SetToDefault() {
	*args = DefaultConfig
}

func New(opts component.Options, args Arguments) (*Component, error) {
	c := &Component{
		l:               opts.Logger,
		onStateChange:   opts.OnStateChange,
		procFS:          args.ProcFS,
		refreshInterval: args.RefreshInterval,
		joinUpdates:     make(chan []discovery.Target),
	}
	return c, nil
}

type Component struct {
	l             log.Logger
	onStateChange func(e component.Exports)

	procFS          string
	refreshInterval time.Duration

	processes   []discovery.Target
	join        []discovery.Target
	joinUpdates chan []discovery.Target
}

func (c *Component) Run(ctx context.Context) error {
	processes, err := discover(c.l, c.procFS)
	if err != nil {
		return err
	}
	c.processes = convertProcesses(processes)
	c.changed()

	t := time.NewTicker(c.refreshInterval)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			processes, err = discover(c.l, c.procFS)
			if err != nil {
				return err
			}
			c.processes = convertProcesses(processes)
			c.changed()
		case jt := <-c.joinUpdates:
			c.join = jt
			c.changed()
		}
	}
}

func (c *Component) Update(args component.Arguments) error {
	c.joinUpdates <- args.(Arguments).Join
	return nil
}

func (c *Component) changed() {
	c.onStateChange(discovery.Exports{
		Targets: join(c.processes, c.join),
	})
}
