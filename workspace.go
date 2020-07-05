package main

import (
	"bytes"
	"errors"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/simon987/task_tracker/api"
	"github.com/simon987/task_tracker/storage"
	"go.uber.org/zap"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Workspace struct {
	Name    string
	Project *storage.Project
	ctx     *DroneContext
	repo    *git.Repository
}

func (w *Workspace) Cleanup() error {
	logger.Info("Cleaning up workspace", zap.String("name", w.Name))

	clonePath := filepath.Join(w.ctx.WorkdirPath, w.Project.Name, w.Name)
	return os.RemoveAll(clonePath)
}

func (w *Workspace) clonePath() string {
	ret, _ := filepath.Abs(filepath.Join(w.ctx.WorkdirPath, w.Name, w.Project.Name+"_"+w.Project.Version))
	return ret
}

func (w *Workspace) Deploy() error {

	if w.repo != nil {
		return errors.New("project is already deployed")
	}

	clonePath := w.clonePath()
	if _, err := os.Stat(clonePath); err == nil {
		w.repo, err = git.PlainOpen(clonePath)
		if err == nil {
			return nil
		}
		err = os.RemoveAll(clonePath)
		if err != nil {
			return err
		}
	}

	logger.Info("Deploying project", zap.String("name", w.Project.Name))

	var err error
	w.repo, err = git.PlainClone(clonePath, false, &git.CloneOptions{
		URL:               w.Project.CloneUrl,
		SingleBranch:      true,
		RecurseSubmodules: 10,
		Progress:          os.Stdout,
		Depth:             1,
	})
	if err != nil {
		return err
	}

	return w.checkout(w.Project.Version)
}

func (w *Workspace) checkout(hash string) error {
	logger.Info("Checking out commit hash", zap.String("version", w.Project.Version))
	worktree, err := w.repo.Worktree()
	if err != nil {
		return err
	}

	err = worktree.Checkout(&git.CheckoutOptions{
		Hash: plumbing.NewHash(hash),
	})
	if err != nil {
		return err
	}
	return nil
}

func (w *Workspace) reset() error {
	logger.Debug("Reset workspace")
	worktree, err := w.repo.Worktree()
	if err != nil {
		return err
	}

	err = worktree.Reset(&git.ResetOptions{
		Mode: git.HardReset,
	})

	if err != nil {
		logger.Error("error during reset", zap.Error(err))
	}
	return err
}

func (w *Workspace) Update() error {

	if w.repo == nil {
		err := w.Deploy()
		if err != nil {
			return err
		}
	}

	ref, err := w.repo.Head()
	if err != nil {
		return err
	}

	if ref.Hash().String() != w.Project.Version {
		err = w.repo.Fetch(&git.FetchOptions{})
		if err != nil {
			return err
		}
		return w.checkout(w.Project.Version)
	}

	return nil
}

func (w *Workspace) Execute(task *storage.Task) error {

	if task.Project.CloneUrl == "" {
		logger.Warn("project does not have clone URL, skipping task")
		_, err := w.ctx.client.ReleaseTask(api.ReleaseTaskRequest{
			TaskId: task.Id,
			Result: storage.TR_SKIP,
		})
		if err != nil {
			return nil
		}
	}

	err := w.Update()
	if err != nil {
		return err
	}

	defer func() { _ = w.reset() }()

	clonePath := w.clonePath()
	runFilePath := filepath.Join(clonePath, "run")

	if _, err := os.Stat(runFilePath); os.IsNotExist(err) {
		time.Sleep(time.Second * 5)
		return errors.New("/run does not exist")
	}

	var stderr bytes.Buffer
	var stdout bytes.Buffer

	cmd := exec.Command(runFilePath)
	cmd.Dir = clonePath
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(cmd.Env, "PROJECT_NAME="+task.Project.Name)
	cmd.Env = append(cmd.Env, "PROJECT_SECRET="+w.ctx.Secrets[task.Project.Id])
	cmd.Env = append(cmd.Env, "TASK_RECIPE="+task.Recipe)

	err = cmd.Run()

	if len(stderr.Bytes()) > 0 {
		err := w.ctx.client.Log(storage.ERROR, stderr.String())
		if err != nil {
			return err
		}
	}

	if len(stdout.Bytes()) > 0 {
		err := w.ctx.client.Log(storage.INFO, stdout.String())
		if err != nil {
			return err
		}
	}

	var result storage.TaskResult
	if err != nil || cmd.ProcessState.ExitCode() != 0 {
		logger.Error(
			"Failed to execute task",
			zap.Int("exitCode", cmd.ProcessState.ExitCode()),
			zap.Error(err),
		)
		result = storage.TR_FAIL
	} else {
		result = storage.TR_OK
	}

	resp, err := w.ctx.client.ReleaseTask(api.ReleaseTaskRequest{
		TaskId:       task.Id,
		Result:       result,
		Verification: 0,
	})
	if err != nil {
		return err
	}

	if !resp.Ok {
		return errors.New(resp.Message)
	}

	return nil
}
