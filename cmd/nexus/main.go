package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("nexus-payments: booting (Task 1 stub)")
	<-ctx.Done()
	fmt.Println("nexus-payments: shutdown complete")
}
