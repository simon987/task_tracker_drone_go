package main

import (
	tt "github.com/simon987/task_tracker/client"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
	"log"
	"os"
)

var logger *zap.Logger

func drone(c *cli.Context) error {

	client := tt.New(c.String("api-url"))

	worker, err := client.MakeWorker(c.String("alias"))
	if err != nil {
		logger.Error("Could not create client", zap.Error(err))
	}

	client.SetWorker(worker)

	projects, err := client.GetProjectList()
	for _, p := range projects {
		logger.Info(p.Name)
	}

	return nil
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
				Name: "api-url",
				Required: true,
				Usage: "task_tracker api url",
			},
			&cli.StringFlag{
				Name: "alias",
				Required: true,
				Usage: "task_tracker worker alias",
			},
		},
	}

	logger, _ = zap.NewProduction()

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
