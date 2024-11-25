package e2e

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	PublicURL  = "http://localhost:3000/api"
	PrivateURL = "http://localhost:3001/api"
)

func isOnline() bool {
	for _, v := range []string{PublicURL, PrivateURL} {
		_, err := resty.New().
			SetBaseURL(v).
			SetTimeout(100 * time.Millisecond).
			R().
			Get("/health")

		if err != nil {
			log.Println("waiting for health", err)
			return false
		}
	}

	return true
}

func TestMain(m *testing.M) {
	log.Println("running e2e tests")

	if _, ok := os.LookupEnv("CI"); !ok {
		if err := exec.Command("docker", "compose", "up", "-d", "--build").Run(); err != nil {
			log.Fatal(fmt.Errorf("docker-compose up -d: %w", err))
		}

		defer func() {
			if err := exec.Command("docker", "compose", "down", "-v").Run(); err != nil {
				log.Fatal(fmt.Errorf("docker-compose down -v: %w", err))
			}
			log.Println("e2e tests finished")
		}()
	}

	startedAt := time.Now()
	for {
		if time.Since(startedAt) > 20*time.Second {
			log.Println("timeout")
			return
		}

		if isOnline() {
			log.Println("e2e tests started")
			break
		}

		time.Sleep(1 * time.Second)
	}

	m.Run()
}