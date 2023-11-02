package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	httpgo "net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/snappy"
	"github.com/gorilla/mux"

	"github.com/grafana/agent/component"
	"github.com/grafana/agent/service/http"
)

func init() {
	component.Register(component.Registration{
		Name:    "prometheus.test.metrics",
		Args:    Arguments{},
		Exports: Exports{},

		NeedsServices: []string{http.ServiceName},
		Build: func(opts component.Options, args component.Arguments) (component.Component, error) {
			return NewComponent(opts, args.(Arguments))
		},
	})
}

type Component struct {
	mut        sync.Mutex
	args       Arguments
	instances  []*instance
	argsUpdate chan struct{}
	path       string
	handler    http.Data
}

// Handler should return a valid HTTP handler for the component.
// All requests to the component will have the path trimmed such that the component is at the root.
func (c *Component) Handler() httpgo.Handler {
	r := mux.NewRouter()
	r.HandleFunc("/discovery", c.discovery)
	r.HandleFunc("/instance/{id}/metrics", c.serveMetrics)
	return r
}

func (c *Component) discovery(w httpgo.ResponseWriter, r *httpgo.Request) {
	w.Header().Set("Content-Type", "application/json")
	instances := make([]target, len(c.instances))
	for x := range c.instances {
		instances[x] = createTarget(c.handler.HTTPListenAddr, c.path+fmt.Sprintf("instance/%d/metrics", x))
	}
	marshalledBytes, _ := json.Marshal(instances)
	_, _ = w.Write(marshalledBytes)
}

func (c *Component) serveMetrics(w httpgo.ResponseWriter, r *httpgo.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		w.WriteHeader(httpgo.StatusNotFound)
		return
	}
	w.Header().Set("Content-Encoding", "snappy")
	_, _ = w.Write(c.instances[id].buffer())
}

func NewComponent(o component.Options, c Arguments) (*Component, error) {
	data, err := o.GetServiceData(http.ServiceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get HTTP information: %w", err)
	}
	httpData := data.(http.Data)
	fullpath := httpData.HTTPPathForComponent(o.ID)
	return &Component{
		args:       c,
		path:       fullpath,
		argsUpdate: make(chan struct{}),
		instances:  make([]*instance, 0),
		handler:    httpData,
	}, nil
}

func (c *Component) Run(ctx context.Context) error {
	c.generateNewSet(true)
	for {
		c.mut.Lock()
		t := time.NewTicker(c.args.MetricsRefresh)
		c.mut.Unlock()
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			{
				c.generateNewSet(false)
			}
		case <-c.argsUpdate:
			{
				// Mainly to update the args.
			}
		}
	}
}

func (c *Component) Update(args component.Arguments) error {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.args = args.(Arguments)
	c.argsUpdate <- struct{}{}
	return nil
}

type Arguments struct {
	NumberOfInstances int           `river:"number_of_instances,attr"`
	NumberOfMetrics   int           `river:"number_of_metrics,attr"`
	NumberOfSeries    int           `river:"number_of_labels,attr"`
	MetricsRefresh    time.Duration `river:"metrics_refresh,attr"`
	ChurnPercent      float32       `river:"churn_percent,attr"`
}

type Exports struct {
	Targets []map[string]string `river:"targets,attr,optional"`
}

// generateNewSet  creates the buffers of data. forceNewInstances instatiates all new buffers.
func (c *Component) generateNewSet(forceNewInstances bool) {
	c.mut.Lock()
	defer c.mut.Unlock()

	if len(c.instances) == 0 || forceNewInstances {
		c.instances = make([]*instance, c.args.NumberOfInstances)
		for i := 0; i < len(c.instances); i++ {
			c.instances[i] = &instance{
				start: 1,
				end:   c.args.NumberOfMetrics,
				id:    i,
			}
		}
	} else {
		for _, i := range c.instances {
			i.churn(c.args.ChurnPercent)
		}
	}
	for _, i := range c.instances {
		i.generateData(c.args.NumberOfSeries)
	}
}

type instance struct {
	mut   sync.RWMutex
	start int
	end   int
	id    int
	buf   []byte
}

func (i *instance) buffer() []byte {
	i.mut.RLock()
	defer i.mut.RUnlock()

	retBuf := make([]byte, len(i.buf))
	copy(retBuf, i.buf)
	return retBuf
}

func (i *instance) churn(churn float32) {
	i.mut.Lock()
	defer i.mut.Unlock()

	if churn == 0 || churn > 1 {
		return
	}
	// This adjusts the ids by the churn rate, so if there was 10 ids and 10% churn it would move them forward one.
	adjust := int(float32(i.end-i.start) * churn)
	i.start = i.start + adjust
	i.end = i.end + adjust
}

func (i *instance) generateData(seriesCount int) {
	i.mut.Lock()
	defer i.mut.Unlock()

	buf := bytes.NewBuffer(nil)
	for j := i.start; j < i.end; j++ {
		buf.WriteString(fmt.Sprintf("# TYPE agent_metric_test_%d counter\n", j))
		buf.WriteString(fmt.Sprintf("agent_metric_test_%d{", j))
		series := make([]string, 0)
		for s := 0; s < seriesCount; s++ {
			series = append(series, fmt.Sprintf("series_%d=\"value_%d\"", s, s))
		}
		series = append(series, fmt.Sprintf("instance_id=\"%d\"", i.id))
		lblstring := strings.Join(series, ",")
		buf.WriteString(lblstring)
		buf.WriteString("} 1\n")
	}
	snappy.Encode(i.buf, buf.Bytes())
}

func createTarget(host, path string) target {
	return target{
		Host: []string{host},
		Labels: map[string]string{
			"__metrics_path__": path,
		},
	}
}

type target struct {
	Host   []string          `json:"targets"`
	Labels map[string]string `json:"labels"`
}

var _ http.Component = (*Component)(nil)
