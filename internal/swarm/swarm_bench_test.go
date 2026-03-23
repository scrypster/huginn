package swarm_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/scrypster/huginn/internal/swarm"
)

func BenchmarkSwarmRun10Tasks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := swarm.NewSwarm(10)
		tasks := make([]swarm.SwarmTask, 10)
		for j := range tasks {
			tasks[j] = swarm.SwarmTask{
				ID:   fmt.Sprintf("task%d", j),
				Name: fmt.Sprintf("Task %d", j),
				Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
					return nil
				},
			}
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range s.Events() {
			}
		}()
		if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
			b.Fatalf("Run: %v", err)
		}
		<-done
	}
}

func BenchmarkSwarmRun50Tasks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := swarm.NewSwarm(16)
		tasks := make([]swarm.SwarmTask, 50)
		for j := range tasks {
			tasks[j] = swarm.SwarmTask{
				ID:   fmt.Sprintf("task%d", j),
				Name: fmt.Sprintf("Task %d", j),
				Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
					return nil
				},
			}
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range s.Events() {
			}
		}()
		if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
			b.Fatalf("Run: %v", err)
		}
		<-done
	}
}

func BenchmarkSwarmRun100Tasks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := swarm.NewSwarm(16)
		tasks := make([]swarm.SwarmTask, 100)
		for j := range tasks {
			tasks[j] = swarm.SwarmTask{
				ID:   fmt.Sprintf("task%d", j),
				Name: fmt.Sprintf("Task %d", j),
				Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
					return nil
				},
			}
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range s.Events() {
			}
		}()
		if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
			b.Fatalf("Run: %v", err)
		}
		<-done
	}
}

func BenchmarkSwarmRun500Tasks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := swarm.NewSwarm(32)
		tasks := make([]swarm.SwarmTask, 500)
		for j := range tasks {
			tasks[j] = swarm.SwarmTask{
				ID:   fmt.Sprintf("task%d", j),
				Name: fmt.Sprintf("Task %d", j),
				Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
					return nil
				},
			}
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range s.Events() {
			}
		}()
		if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
			b.Fatalf("Run: %v", err)
		}
		<-done
	}
}
