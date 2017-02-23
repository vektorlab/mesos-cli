package commands

import (
	"bytes"
	"fmt"
	"github.com/mesos/mesos-go"
	"github.com/mesos/mesos-go/httpcli"
	"github.com/mesos/mesos-go/httpcli/operator"
	master "github.com/mesos/mesos-go/master/calls"
	"github.com/vektorlab/mesos-cli/config"
	"github.com/vektorlab/mesos-cli/filter"
	"go.uber.org/zap"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
)

func newCaller(endpoint *url.URL, log *zap.Logger) operator.Caller {
	return operator.NewCaller(
		httpcli.New(
			httpcli.Endpoint(endpoint.String()),
			httpcli.RequestOptions(func(req *http.Request) {
				buf, _ := ioutil.ReadAll(req.Body)
				req.Body.Close()
				log.Debug(
					"http request",
					zap.String("url", req.URL.String()),
					zap.String("body", string(buf)),
				)
				req.Body = ioutil.NopCloser(bytes.NewBuffer(buf))
			},
			),
		))
}

func NewCaller(profile *config.Profile) operator.Caller {
	return newCaller(profile.Endpoint(), profile.Log())
}

func NewAgentCaller(profile *config.Profile, id string) (operator.Caller, error) {
	resp, err := NewCaller(profile).CallMaster(master.GetAgents())
	if err != nil {
		return nil, err
	}
	agent, err := filter.AsAgent(filter.FromMaster(resp).FindOne(filter.AgentIDFilter(id, false)))
	if err != nil {
		return nil, err
	}
	endpoint := profile.Endpoint()
	endpoint.Host = fmt.Sprintf("%s:%d", agent.Hostname, agent.GetPort())
	endpoint.Path = fmt.Sprintf("slave(1)%s", config.OperatorAPIPath)
	return newCaller(endpoint, profile.Log()), nil
}

func Scalar(name string, resources mesos.Resources) (v float64) {
	if scalar := resources.SumScalars(mesos.NamedResources(name)); scalar != nil {
		v = scalar.Value
	}
	return v
}

func truncStr(s string, l int) string {
	runes := bytes.Runes([]byte(s))
	if len(runes) < l {
		return s
	}
	return string(runes[:l])
}

func failOnErr(err error) {
	if err != nil {
		fmt.Printf("Encountered Error: %v\n", err)
		os.Exit(2)
	}
}
