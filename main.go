package main

import (
	"github.com/zerodotfive/presenced/service"
	"github.com/spf13/cobra"
	"os/signal"
	"fmt"
	"os"
	"syscall"
)

type Config struct {
	ConfigFile string
}

func (self *Config) Run() error {
	var err error
	var services map[string]*service.Service
	services, err = service.Load(self.ConfigFile)
	if err != nil {
		return err
	}

	mySigChan := make(chan os.Signal, 1)
	signal.Notify(mySigChan, syscall.SIGINT)
	signal.Notify(mySigChan, syscall.SIGTERM)

	for s := range services {
		services[s].Start()
	}

	mySig := <-mySigChan
	fmt.Printf("Got signal %s. Waiting for children to exit.\n", mySig)
	for s := range services {
		if <-services[s].ExitChan {
			continue
		}
	}
	return nil
}

func main() {
	config := &Config{}

	program := &cobra.Command{
		Example:  "presenced -c config.conf",
		Run: func(program *cobra.Command, args []string) {
			err := config.Run()
			if err != nil {
				fmt.Printf("%#v\n", err)
				os.Exit(1)
			}
			return
		},
	}

	program.Flags().StringVarP(&config.ConfigFile, "config", "c", "/etc/presenced.conf", "Configfile")
	program.Execute()
}
