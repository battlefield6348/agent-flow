package orchestrator

import (
"fmt"
"os"
"os/exec"
"time"
)

type Worker struct {
	Config CollaboratorConfig
	cmd    *exec.Cmd // 🔴 必須保存 cmd 實例
	stopCh chan struct{}
}

func NewWorker(cfg CollaboratorConfig) *Worker {
	return &Worker{
		Config: cfg,
		stopCh: make(chan struct{}),
	}
}

func (w *Worker) Start() {
	go w.runLoop()
}

func (w *Worker) runLoop() {
	for {
		select {
		case <-w.stopCh:
			return
		default:
			w.runProcess()
			time.Sleep(5 * time.Second)
		}
	}
}

func (w *Worker) runProcess() {
	args := append([]string{}, w.Config.Args...)
	if w.Config.InitialInstruction != "" {
		args = append(args, "--prompt", w.Config.InitialInstruction)
	}

	w.cmd = exec.Command(w.Config.Cmd, args...) // 🔴 分派給成員變數
	w.cmd.Env = os.Environ()
	for k, v := range w.Config.Env {
		w.cmd.Env = append(w.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	w.cmd.Stdout = os.Stdout
	w.cmd.Stderr = os.Stderr

	if err := w.cmd.Start(); err != nil {
		fmt.Printf("[%s] failed to start: %v\n", w.Config.ID, err)
		return
	}

	fmt.Printf("%s [Worker:%s] Engine started\n", time.Now().Format("2006/01/02 15:04:05"), w.Config.ID)

	_ = w.cmd.Wait()
}

func (w *Worker) Stop() {
	close(w.stopCh)
	if w.cmd != nil && w.cmd.Process != nil {
		fmt.Printf("Killing process for %s...\n", w.Config.ID)
		w.cmd.Process.Signal(os.Interrupt) // 🟢 正確發送訊號
	}
}

type WorkerManager struct {
	Workers []*Worker
}

func NewWorkerManager(configs []CollaboratorConfig) *WorkerManager {
	var workers []*Worker
	for _, cfg := range configs {
		workers = append(workers, NewWorker(cfg))
	}
	return &WorkerManager{Workers: workers}
}

func (m *WorkerManager) StartAll() {
	for _, w := range m.Workers {
		w.Start()
	}
}

func (m *WorkerManager) StopAll() {
	for _, w := range m.Workers {
		w.Stop()
	}
}
