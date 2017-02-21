package config

import (
	"encoding/json"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/mesos/mesos-go"
	"github.com/satori/go.uuid"
	"io/ioutil"
	"net/url"
	"os"
	"os/user"
	"strings"
)

const (
	OperatorAPIPath  = "/api/v1"
	SchedulerAPIPath = "/api/v1/scheduler"
)

func defaults() *Profile {
	return &Profile{
		Master: "http://localhost:5050",
		TaskInfo: &mesos.TaskInfo{
			TaskID: mesos.TaskID{
				Value: uuid.NewV4().String(),
			},
			Command: &mesos.CommandInfo{
				Environment: &mesos.Environment{
					Variables: []mesos.Environment_Variable{},
				},
			},
			Container: &mesos.ContainerInfo{
				Type:    mesos.ContainerInfo_MESOS.Enum(),
				Volumes: []mesos.Volume{},
			},
			Resources: []mesos.Resource{
				mesos.Resource{
					Name:   "cpus",
					Type:   mesos.SCALAR.Enum(),
					Role:   proto.String("*"),
					Scalar: &mesos.Value_Scalar{Value: 0.1},
				},
			},
			Labels: &mesos.Labels{},
		},
	}
}

// Profile contains environment specific options
type Profile struct {
	Master   string `json:"master"`
	TaskInfo *mesos.TaskInfo
}

// Options are functional profile options
type Option func(*Profile)

// Master specifies the Mesos master hostname
func Master(master string) Option {
	return func(p *Profile) {
		if master != "" {
			p.Master = master
		}
	}
}

type CommandOpts struct {
	Shell bool
	User  string
	Value string
	Envs  []mesos.Environment_Variable
}

// Command sets Mesos CommandInfo options
func Command(opts CommandOpts) Option {
	return func(p *Profile) {
		if opts.User != "" {
			p.TaskInfo.Command.User = proto.String(opts.User)
		}
		p.TaskInfo.Command.Shell = proto.Bool(opts.Shell)
		if opts.Value != "" {
			if opts.Shell {
				p.TaskInfo.Command.Value = proto.String(opts.Value)
			} else {
				p.TaskInfo.Command.Arguments = strings.Split(opts.Value, " ")
			}
		}
		for _, env := range opts.Envs {
			p.TaskInfo.Command.Environment.Variables = append(p.TaskInfo.Command.Environment.Variables, env)
		}
	}
}

type ContainerOpts struct {
	Docker bool
	//Image  *mesos.Image
	Image string
	// Docker specific opts
	Privileged     bool
	ForcePullImage bool
	NetworkMode    mesos.ContainerInfo_DockerInfo_Network
	Volumes        []mesos.Volume
	Parameters     []mesos.Parameter
	PortMappings   []mesos.ContainerInfo_DockerInfo_PortMapping
}

func Container(opts ContainerOpts) Option {
	return func(p *Profile) {
		for _, vol := range opts.Volumes {
			p.TaskInfo.Container.Volumes = append(p.TaskInfo.Container.Volumes, vol)
		}
		if !opts.Docker {
			// TODO: Support Docker/appc images for "universal" containerizer
			return
		}
		// All Docker-specific opts below
		p.TaskInfo.Container.Type = mesos.ContainerInfo_DOCKER.Enum()
		p.TaskInfo.Container.Docker = &mesos.ContainerInfo_DockerInfo{}
		if opts.Image != "" {
			p.TaskInfo.Container.Docker.Image = opts.Image
		}
		for _, param := range opts.Parameters {
			p.TaskInfo.Container.Docker.Parameters = append(p.TaskInfo.Container.Docker.Parameters, param)
		}
		for _, mapping := range opts.PortMappings {
			// Append a port resource for the requested host port
			// The host port must be (by Mesos default) between 31000-32000
			p.TaskInfo.Resources = append(p.TaskInfo.Resources, portOffer(mapping.HostPort))
			p.TaskInfo.Container.Docker.PortMappings = append(p.TaskInfo.Container.Docker.PortMappings, mapping)
		}
		p.TaskInfo.Container.Docker.Network = opts.NetworkMode.Enum()
		p.TaskInfo.Container.Docker.Privileged = proto.Bool(opts.Privileged)
		p.TaskInfo.Container.Docker.ForcePullImage = proto.Bool(opts.ForcePullImage)
	}
}

func TaskInfo(info *mesos.TaskInfo) Option {
	return func(p *Profile) {
		p.TaskInfo = info
	}
}

func (p *Profile) With(opts ...Option) *Profile {
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p Profile) Endpoint() *url.URL {
	u, _ := url.Parse(p.Master)
	u.Path = OperatorAPIPath
	return u
}

type Config struct {
	Profiles map[string]*Profile `json:profiles`
}

// LoadProfile loads a user configuration
// from ~/.mesos-cli.json creating a
// JSON file with defaults if it does
// not exist.
func LoadProfile(path, name string) (*Profile, error) {
	config := &Config{Profiles: map[string]*Profile{}}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		config.Profiles["default"] = defaults()
		raw, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return nil, err
		}
		return config.Profiles["default"], ioutil.WriteFile(path, raw, os.FileMode(0755))
	}
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	if _, ok := config.Profiles[name]; !ok {
		return nil, fmt.Errorf("Cannot load profile: %s", name)
	}
	if config.Profiles[name].TaskInfo == nil {
		config.Profiles[name].TaskInfo = defaults().TaskInfo
	}
	return config.Profiles[name], nil
}

func portOffer(port uint32) mesos.Resource {
	return mesos.Resource{
		Name:   "ports",
		Type:   mesos.RANGES.Enum(),
		Role:   proto.String("*"),
		Ranges: &mesos.Value_Ranges{Range: []mesos.Value_Range{mesos.Value_Range{Begin: uint64(port), End: uint64(port)}}},
	}
}

func HomeDir() string {
	u, err := user.Current()
	if err != nil {
		panic(err)
	}
	return u.HomeDir
}
