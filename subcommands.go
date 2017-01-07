package main

import (
	"fmt"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/gogo/protobuf/proto"
	"github.com/gosuri/uitable"
	"github.com/jawher/mow.cli"
	mesos "github.com/mesos/mesos-go/mesosproto"
	"regexp"
	"strings"
)

func ps(cmd *cli.Cmd) {
	cmd.Spec = "[OPTIONS]"
	defaults := DefaultProfile()
	var (
		master   = cmd.StringOpt("master", defaults.Master, "Mesos Master")
		limit    = cmd.IntOpt("limit", 100, "maximum number of tasks to return per request")
		max      = cmd.IntOpt("max", 250, "maximum number of tasks to list")
		order    = cmd.StringArg("order", "desc", "accending or decending sort order [asc|desc]")
		name     = cmd.StringOpt("name", "", "regular expression to match the TaskId")
		all      = cmd.BoolOpt("a all", false, "show all tasks")
		running  = cmd.BoolOpt("r running", true, "show running tasks")
		failed   = cmd.BoolOpt("fa failed", false, "show failed tasks")
		killed   = cmd.BoolOpt("k killed", false, "show killed tasks")
		finished = cmd.BoolOpt("f finished", false, "show finished tasks")
	)
	stateFilter := func(name string) Filter {
		return func(t interface{}) bool {
			task := t.(*taskInfo)
			return task.State.String() == name
		}
	}
	Filters := func() []Filter {
		filters := []Filter{}
		if *name != "" {
			exp, err := regexp.Compile(*name)
			failOnErr(err)
			filters = append(filters, func(t interface{}) bool {
				task := t.(*taskInfo)
				return exp.MatchString(task.Name)
			})
		}
		if *all {
			filters = append(filters, func(t interface{}) bool { return true })
			return filters
		}
		if *running {
			filters = append(filters, stateFilter("TASK_RUNNING"))
		}
		if *failed {
			filters = append(filters, stateFilter("TASK_FAILED"))
		}
		if *killed {
			filters = append(filters, stateFilter("TASK_KILLED"))
		}
		if *finished {
			filters = append(filters, stateFilter("TASK_FINISHED"))
		}
		return filters
	}
	cmd.Action = func() {
		tasks := make(chan *taskInfo)
		client := &Client{
			Hostname: config.Profile(WithMaster(*master)).Master,
		}
		paginator := &TaskPaginator{
			limit: *limit,
			max:   *max,
			order: *order,
			tasks: tasks,
		}
		go func() {
			failOnErr(Paginate(client, paginator, Filters()...))
		}()
		table := uitable.New()
		table.AddRow("ID", "FRAMEWORK", "STATE", "CPUS", "MEM", "GPUS", "DISK")
		for task := range tasks {
			table.AddRow(task.ID, truncStr(task.FrameworkID, 8), task.State.String(), task.Resources.CPU, task.Resources.Mem, task.Resources.GPUs, task.Resources.Disk)
		}
		fmt.Println(table)
	}
}

func ls(cmd *cli.Cmd) {
	defaults := DefaultProfile()
	cmd.Spec = "[OPTIONS] ID"
	var (
		master = cmd.StringOpt("master", defaults.Master, "Mesos Master")
		taskID = cmd.StringArg("ID", "", "Task to list")
	)
	cmd.Action = func() {
		client := &Client{
			Hostname: config.Profile(WithMaster(*master)).Master,
		}
		// First attempt to resolve the task by ID
		task, err := FindTask(*taskID, client)
		failOnErr(err)
		// Attempt to get the full agent state
		agent, err := Agent(client, task.AgentID)
		failOnErr(err)
		// Lookup executor information in agent state
		executor := findExecutor(agent, task.ID)
		if executor == nil {
			failOnErr(fmt.Errorf("could not resolve executor"))
		}
		fmt.Println(executor.Directory)
	}
}
func agents(cmd *cli.Cmd) {
	defaults := DefaultProfile()
	cmd.Spec = "[OPTIONS]"
	var master = cmd.StringOpt("master", defaults.Master, "Mesos Master")
	cmd.Action = func() {
		client := &Client{
			Hostname: config.Profile(WithMaster(*master)).Master,
		}
		agents, err := Agents(client)
		failOnErr(err)
		table := uitable.New()
		table.AddRow("ID", "FQDN", "VERSION", "UPTIME", "CPUS", "MEM", "GPUS", "DISK")
		for _, agent := range agents {
			table.AddRow(
				agent.ID,
				agent.FQDN(),
				agent.Version,
				agent.Uptime().String(),
				fmt.Sprintf("%.2f/%.2f", agent.UsedResources.CPU, agent.Resources.CPU),
				fmt.Sprintf("%.2f/%.2f", agent.UsedResources.Mem, agent.Resources.Mem),
				fmt.Sprintf("%.2f/%.2f", agent.UsedResources.GPUs, agent.Resources.GPUs),
				fmt.Sprintf("%.2f/%.2f", agent.UsedResources.Disk, agent.Resources.Disk),
			)
		}
		fmt.Println(table)
	}
}

func exec(cmd *cli.Cmd) {
	cmd.Spec = "[OPTIONS] [ARG...]"
	defaults := DefaultProfile()
	var (
		master     = cmd.StringOpt("master", defaults.Master, "Mesos Master")
		arguments  = cmd.StringsArg("ARG", nil, "Command Arguments")
		taskPath   = cmd.StringOpt("task", "", "Path to a Mesos TaskInfo JSON file")
		parameters = cmd.StringsOpt("param", []string{}, "Docker parameters")
		image      = cmd.StringOpt("i image", "", "Docker image to run")
		volumes    = cmd.StringsOpt("v volume", []string{}, "Volume mappings")
		ports      = cmd.StringsOpt("p ports", []string{}, "Port mappings")
		envs       = cmd.StringsOpt("e env", []string{}, "Environment Variables")
		shell      = cmd.StringOpt("s shell", "", "Shell command to execute")
	)
	task := NewTask()
	cmd.VarOpt(
		"n name",
		str{pt: task.Name},
		"Task Name",
	)
	cmd.VarOpt(
		"u user",
		str{pt: task.Command.User},
		"User to run as",
	)
	cmd.VarOpt(
		"c cpus",
		flt{pt: task.Resources[0].Scalar.Value},
		"CPU Resources to allocate",
	)
	cmd.VarOpt(
		"m mem",
		flt{pt: task.Resources[1].Scalar.Value},
		"Memory Resources (mb) to allocate",
	)
	cmd.VarOpt(
		"d disk",
		flt{pt: task.Resources[2].Scalar.Value},
		"Disk Resources (mb) to allocate",
	)
	cmd.VarOpt(
		"privileged",
		bl{pt: task.Container.Docker.Privileged},
		"Give extended privileges to this container",
	)
	cmd.VarOpt(
		"f forcePullImage",
		bl{pt: task.Container.Docker.ForcePullImage},
		"Always pull the container image",
	)

	cmd.Before = func() {
		if *shell != "" {
			task.Command.Shell = proto.Bool(true)
			task.Command.Value = shell
		} else {
			for _, arg := range *arguments {
				*task.Command.Value += fmt.Sprintf(" %s", arg)
			}
		}
		if *taskPath != "" {
			failOnErr(TaskFromJSON(task, *taskPath))
		}
		failOnErr(setPorts(task, *ports))
		failOnErr(setVolumes(task, *volumes))
		failOnErr(setParameters(task, *parameters))
		failOnErr(setEnvironment(task, *envs))
		// Assuming that if image is specified the user wants
		// to run with the Docker containerizer. This is
		// not always the case as an image may be passed
		// to the Mesos containerizer as well.
		if *image != "" {
			task.Container.Mesos = nil
			task.Container.Type = mesos.ContainerInfo_DOCKER.Enum()
			task.Container.Docker.Image = image
		} else {
			task.Container.Docker = nil
		}
		// Nothing to do if not running a container
		// and no arguments are specified.
		if *image == "" && *taskPath == "" && len(*arguments) == 0 && *shell == "" {
			cmd.PrintHelp()
			cli.Exit(1)
		}
	}
	cmd.Action = func() {
		failOnErr(RunTask(config.Profile(WithMaster(*master)), task))
	}
}

const (
	repository    string = "quay.io/vektorcloud/mesos:latest"
	containerName string = "mesos_cli"
)

// local attempts to launch a local Mesos cluster
// with github.com/vektorcloud/mesos.
func local(cmd *cli.Cmd) {
	var (
		container *docker.APIContainers
		image     *docker.APIImages
	)
	cmd.Spec = "[OPTIONS]"
	up := func(cmd *cli.Cmd) {
		var (
			remove = cmd.BoolOpt("rm remove", false, "Remove any existing local cluster")
			force  = cmd.BoolOpt("f force", false, "Force pull a new image from vektorcloud")
		)
		cmd.Action = func() {
			client, err := docker.NewClientFromEnv()
			failOnErr(err)
			image = getImage(repository, client)
			if image == nil || *force {
				failOnErr(client.PullImage(docker.PullImageOptions{Repository: repository}, docker.AuthConfiguration{}))
			}
			image = getImage(repository, client)
			if image == nil {
				failOnErr(fmt.Errorf("Cannot pull image %s", repository))
			}
			container = getContainer(containerName, client)
			if container != nil && *remove {
				failOnErr(client.RemoveContainer(docker.RemoveContainerOptions{ID: container.ID, Force: true}))
				container = nil
			}
			if container == nil {
				_, err = client.CreateContainer(
					docker.CreateContainerOptions{
						Name: containerName,
						HostConfig: &docker.HostConfig{
							NetworkMode: "host",
							Binds: []string{
								"/var/run/docker.sock:/var/run/docker.sock:rw",
							},
						},
						Config: &docker.Config{
							Cmd:   []string{"mesos-local"},
							Image: repository,
						}})
				failOnErr(err)
				container = getContainer(containerName, client)
			}
			failOnErr(client.StartContainer(container.ID, &docker.HostConfig{}))
		}
	}
	down := func(cmd *cli.Cmd) {
		cmd.Action = func() {
			client, err := docker.NewClientFromEnv()
			failOnErr(err)
			if container = getContainer(containerName, client); container != nil {
				if container.State != "running" {
					fmt.Printf("container is in invalid state: %s\n", container.State)
					cli.Exit(1)
				}
			}
			fmt.Println("no countainer found")
			cli.Exit(1)
		}
	}
	status := func(cmd *cli.Cmd) {
		cmd.Action = func() {
			client, err := docker.NewClientFromEnv()
			failOnErr(err)
			if container = getContainer(containerName, client); container != nil {
				fmt.Printf("%s: %s\n", container.ID, container.State)
			} else {
				fmt.Println("no container found")
			}
			cli.Exit(0)
		}
	}
	rm := func(cmd *cli.Cmd) {
		cmd.Action = func() {
			client, err := docker.NewClientFromEnv()
			failOnErr(err)
			if container = getContainer(containerName, client); container != nil {
				fmt.Printf("removing container %s\n", container.ID)
				failOnErr(client.RemoveContainer(docker.RemoveContainerOptions{ID: container.ID, Force: true}))
				cli.Exit(0)
			}
			fmt.Println("no container found")
			cli.Exit(1)
		}
	}
	cmd.Command("up", "Start the local cluster", up)
	cmd.Command("down", "Stop the local cluster", down)
	cmd.Command("status", "Display the status of the local cluster", status)
	cmd.Command("rm", "Remove the local cluster", rm)
}

func getImage(n string, client *docker.Client) *docker.APIImages {
	images, err := client.ListImages(docker.ListImagesOptions{All: true})
	failOnErr(err)
	for _, image := range images {
		for _, tag := range image.RepoTags {
			if tag == n {
				return &image
			}
		}
	}
	return nil
}

func getContainer(n string, client *docker.Client) *docker.APIContainers {
	containers, err := client.ListContainers(docker.ListContainersOptions{All: true})
	failOnErr(err)
	for _, container := range containers {
		for _, name := range container.Names {
			if strings.Replace(name, "/", "", 1) == n {
				return &container
			}
		}
	}
	return nil
}
