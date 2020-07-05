package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/simon987/task_tracker/api"
	tt "github.com/simon987/task_tracker/client"
	"github.com/simon987/task_tracker/storage"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
	"io/ioutil"
	"log"
	"os"
	"time"
)

type DroneContext struct {
	WorkdirPath string
	client      *tt.TaskTrackerClient
	Projects    []storage.Project
	Secrets     map[int64]string
}

func (ctx *DroneContext) FetchProjects() error {

	logger.Debug("fetching project list")
	projects, err := ctx.client.GetProjectList()
	if err != nil {
		return err
	}

	for _, p := range projects {
		logger.Debug("got project", zap.String("name", p.Name), zap.String("version", p.Version))

		secret, err := ctx.client.GetProjectSecret(int(p.Id))
		if err != nil {
			return err
		}

		ctx.Secrets[p.Id] = secret
	}

	ctx.Projects = projects
	return nil
}

var logger *zap.Logger

func (ctx *DroneContext) taskRunner(name string) {

	logger.Info("Starting task runner goroutine", zap.String("name", name))

	for {
		for _, p := range ctx.Projects {
			task, err := ctx.client.FetchTask(int(p.Id))

			if err != nil {
				logger.Error("error fetching task", zap.Error(err))
				continue
			}

			if task.Ok == false {
				if task.Message != "No task available" {
					logger.Error("couldn't fetch task", zap.String("message", task.Message))
					continue
				}

				time.Sleep(time.Second * 1)
				continue
			}

			w := Workspace{
				Name:    name,
				Project: &p,
				ctx:     ctx,
			}
			err = w.Execute(&task.Content.Task)
			if err != nil {
				logger.Error("error executing task", zap.Error(err))
				continue
			}
		}
	}
}

func (ctx *DroneContext) updateProjects() {

	requestedAccess := make(map[int64]bool)

	for {
		err := ctx.FetchProjects()
		if err != nil {
			logger.Error("error while fetching projects", zap.Error(err))
		}

		for _, p := range ctx.Projects {

			ok, _ := requestedAccess[p.Id]
			if !ok {
				logger.Info("requesting access to project", zap.String("name", p.Name))
				_, err := ctx.client.RequestAccess(api.CreateWorkerAccessRequest{
					Assign:  true,
					Submit:  false,
					Project: p.Id,
				})

				if err != nil {
					logger.Error("error requesting access", zap.Error(err))
				} else {
					requestedAccess[p.Id] = true
				}
			}
		}

		time.Sleep(time.Second * 60)
	}
}

func makeWorker(client *tt.TaskTrackerClient, alias string) (*tt.Worker, error) {
	var worker *tt.Worker

	path := fmt.Sprintf("worker_%s.json", alias)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		worker, err = client.MakeWorker(alias)
		if err != nil {
			logger.Error("Could not create client", zap.Error(err))
			return nil, err
		}
		saveWorker(worker)
		return worker, nil
	}

	fp, _ := os.OpenFile(path, os.O_RDONLY, 0600)
	workerJsonData, _ := ioutil.ReadAll(fp)
	err := json.Unmarshal(workerJsonData, &worker)
	if err != nil {
		return nil, err
	}

	logger.Info("loaded worker from file", zap.String("alias", alias))

	return worker, nil
}

func saveWorker(w *tt.Worker) {
	workerJsonData, _ := json.Marshal(&w)

	path := fmt.Sprintf("worker_%s.json", w.Alias)

	fp, _ := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	_, _ = fp.Write(workerJsonData)
}

func drone(c *cli.Context) error {

	err := os.MkdirAll(c.String("workdir"), 0755)
	if err != nil {
		return err
	}

	client := tt.New(c.String("api-url"))

	worker, err := makeWorker(client, c.String("alias"))
	if err != nil {
		return err
	}

	ctx := &DroneContext{WorkdirPath: "workdir", Secrets: make(map[int64]string)}
	client.SetWorker(worker)
	ctx.client = client

	err = ctx.FetchProjects()
	if err != nil {
		logger.Error("error while fetching projects", zap.Error(err))
		return errors.New("could not bootstrap task runner")
	}

	go ctx.updateProjects()
	for i := 0; i < c.Int("concurrency"); i++ {
		runnerName := fmt.Sprintf("%s-%d", c.String("alias"), i)
		go ctx.taskRunner(runnerName)
	}

	for {
		time.Sleep(time.Second)
	}
}

func main() {
	app := &cli.App{
		Name:   "task_tracker_drone_go",
		Usage:  "TODO:",
		Action: drone,
		Authors: []*cli.Author{
			{
				Name:  "simon987",
				Email: "me@simon987.net",
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "api-url",
				Required: true,
				Usage:    "task_tracker api url",
				EnvVars:  []string{"TT_API_URL"},
			},
			&cli.StringFlag{
				Name:     "alias",
				Required: true,
				Usage:    "task_tracker worker alias",
				EnvVars:  []string{"TT_ALIAS"},
			},
			&cli.StringFlag{
				Name:    "workdir",
				Value:   "workdir",
				Usage:   "Work directory name",
				EnvVars: []string{"TT_WORKDIR"},
			},
			&cli.IntFlag{
				Name:    "concurrency",
				Value:   20,
				Usage:   "Number of tasks to execute at the same time",
				EnvVars: []string{"TT_CONCURRENCY"},
			},
		},
	}

	logger, _ = zap.NewProduction()

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
