package main

import (
"bufio"
"fmt"
"log"
"os"
"os/signal"
"strings"
"syscall"
"time"

"gemini-collaborator-go/internal/mcp"
"gemini-collaborator-go/internal/orchestrator"
"gemini-collaborator-go/internal/repository"
)

func main() {
	cfgPath := "configs/config.yaml"
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfgPath = "configs/config.yaml.example"
	}

	cfg, err := orchestrator.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	repo, err := repository.NewSQLiteTaskRepository(cfg.Database.Path)
	if err != nil {
		log.Fatalf("failed to initialize repository: %v", err)
	}

	mcpServer := mcp.NewServer()
	orchestrator.RegisterMCPTools(mcpServer, repo)
	
	go func() {
		_ = mcpServer.Start(":8080")
	}()

	manager := orchestrator.NewWorkerManager(cfg.Collaborators)
	go manager.StartAll()

	fmt.Println("\n====================================================")
	fmt.Println("🚀 Gemini Collaborator Base Center Ready")
	fmt.Println("====================================================")
	fmt.Println("Commands: ")
	fmt.Println("  add <tags> <payload> : Dispatch a manual task")
	fmt.Println("  tasks                : List all tasks status")
	fmt.Println("  exit                 : Shutdown orchestrator")
	fmt.Println("====================================================")

	reader := bufio.NewReader(os.Stdin)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		for {
			fmt.Print("(collaborator) > ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input == "" {
				continue
			}

			parts := strings.SplitN(input, " ", 3)
			cmd := parts[0]

			switch cmd {
			case "add":
				if len(parts) < 3 {
					fmt.Println("Usage: add <tags> <payload> (tags separate by comma)")
					continue
				}
				tags := strings.Split(parts[1], ",")
				err := repo.CreateTask(&repository.Task{
					WorkflowID: fmt.Sprintf("manual-%d", time.Now().Unix()),
					Status:     repository.StatusIdle,
					TargetTags: tags,
					Payload:    parts[2],
				})
				if err != nil {
					fmt.Printf("Error adding task: %v\n", err)
				} else {
					fmt.Println("Task dispatched successfully.")
				}

			case "tasks":
				fmt.Println("Current Task List (Feature Coming Soon)")
			case "exit", "quit":
				sigChan <- syscall.SIGTERM
				return
			default:
				fmt.Println("Unknown command. Try: add, tasks, exit")
			}
		}
	}()

	<-sigChan
	fmt.Println("\nShutting down...")
	manager.StopAll()
	os.Exit(0)
}
