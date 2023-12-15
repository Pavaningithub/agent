package java

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/grafana/agent/component"
	"github.com/grafana/agent/component/discovery"
	"github.com/grafana/agent/component/pyroscope"
	"github.com/grafana/agent/pkg/flow/logging/level"
)

const (
	labelProcessID = "__process_pid__"
)

func init() {
	component.Register(component.Registration{
		Name: "pyroscope.java",
		Args: Arguments{},

		Build: func(opts component.Options, args component.Arguments) (component.Component, error) {
			err := profiler.Extract()
			if err != nil {
				return nil, fmt.Errorf("extract async profiler: %w", err)
			}
			a := args.(Arguments)
			forwardTo := pyroscope.NewFanout(a.ForwardTo, opts.ID, opts.Registerer)
			c := &javaComponent{
				opts:      opts,
				args:      a,
				forwardTo: forwardTo,
			}
			c.updateTargets(a.Targets)
			return c, nil
		},
	})
}

type Arguments struct {
	Targets   []discovery.Target     `river:"targets,attr"`
	ForwardTo []pyroscope.Appendable `river:"forward_to,attr"`

	ProfilingConfig ProfilingConfig `river:"profiling_config,block,optional"`
}

type ProfilingConfig struct {
	Interval   time.Duration `river:"interval,attr,optional"`
	SampleRate int           `river:"sample_rate,attr,optional"`
	Alloc      string        `river:"alloc,attr,optional"`
	Lock       string        `river:"lock,attr,optional"`
	CPU        bool          `river:"cpu,attr,optional"`
}

func (rc *Arguments) UnmarshalRiver(f func(interface{}) error) error {
	*rc = defaultArguments()
	type config Arguments
	return f((*config)(rc))
}

func defaultArguments() Arguments {
	return Arguments{
		ProfilingConfig: ProfilingConfig{
			Interval:   15 * time.Second,
			SampleRate: 100,
			Alloc:      "",
			Lock:       "",
			CPU:        true,
		},
	}
}

type javaComponent struct {
	opts      component.Options
	args      Arguments
	forwardTo *pyroscope.Fanout

	mutex       sync.Mutex
	pid2process map[int]*profilingLoop
}

func (j *javaComponent) Run(ctx context.Context) error {
	defer func() {
		j.stop()
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		}
	}
}

func (j *javaComponent) Update(args component.Arguments) error {
	newArgs := args.(Arguments)
	j.forwardTo.UpdateChildren(newArgs.ForwardTo)
	a := args.(Arguments)
	j.updateTargets(a.Targets)
	return nil
}

// todo debug info

func (j *javaComponent) updateTargets(targets []discovery.Target) {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	active := make(map[int]struct{})
	for _, target := range targets {
		pid, err := strconv.Atoi(target[labelProcessID])
		_ = level.Debug(j.opts.Logger).Log("msg", "active target",
			"target", fmt.Sprintf("%+v", target),
			"pid", pid)
		if err != nil {
			_ = level.Error(j.opts.Logger).Log("msg", "invalid target", "target", fmt.Sprintf("%v", target), "err", err)
			continue
		}
		proc := j.pid2process[pid]
		if proc == nil {
			proc = newProcess(pid, target, j.opts.Logger, j.forwardTo, j.args.ProfilingConfig)
		} else {
			proc.update(target)
		}
		active[pid] = struct{}{}
	}
	for pid := range j.pid2process {
		if _, ok := active[pid]; ok {
			continue
		}
		_ = level.Debug(j.opts.Logger).Log("msg", "inactive target", "pid", pid)
		j.pid2process[pid].Close()
		delete(j.pid2process, pid)
	}
}

func (j *javaComponent) stop() {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	for _, proc := range j.pid2process {
		proc.Close()
		delete(j.pid2process, proc.pid)
	}
}
