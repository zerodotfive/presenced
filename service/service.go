package service

import (
	"time"
	"github.com/BurntSushi/toml"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"net/http"
	"bytes"
	"os"
	"net"
	"strings"
	"os/signal"
	"syscall"
	"fmt"
	"strconv"
)

type rawService struct {
	EtcdServer	[]string
	KeyPrefix	string
	EtcdKeyTTL	string
	EtcdKeyFreq	string
	EtcdTimeout	string
	CheckType	string
	CheckTCP	string
	CheckHTTP	string
	CheckHTTPCode	string
	CheckTimeout	string
	ExposeIface	string
	ExposeIPaddr	string
	ExposePort	string
	WeightActive	string
	TTLOnExit	string
}


type methodCheck func()(bool)

type Service struct {
	EtcdServer	[]string
	KeyName		string
	EtcdKeyTTL	time.Duration
	EtcdKeyFreq	time.Duration
	EtcdTimeout	time.Duration
	exposeIPaddr	string
	writeActive	string
	writeExiting	string
	TTLOnExit	time.Duration

	checkParams	string
	Check		methodCheck
	CheckTimeout	time.Duration
	CheckHTTPCode	int

	SigChan		chan os.Signal
	ExitChan	chan bool
	keysApi		client.KeysAPI
}

func (self *Service)checkTCP()(bool) {
	_, err := net.DialTimeout("tcp", self.checkParams, self.CheckTimeout)
	if err != nil {
		return true
	}
	return false
}

func (self *Service)checkHTTP()(bool) {
	client := http.Client{
		Timeout: self.CheckTimeout,
	}
	resp, err := client.Get(self.checkParams)
	if err != nil {
		return true
	}
	if resp.StatusCode != self.CheckHTTPCode {
		return true
	}

	return false
}

func (self *Service)CreateFrom(raw rawService)(error) {
	var err error
	self.EtcdServer = raw.EtcdServer
	if len(self.EtcdServer) == 0 {
		return fmt.Errorf("Couldn't find etcdServer.\n")
	}

	var bufferKeyPrefix bytes.Buffer
	bufferKeyPrefix.WriteString(raw.KeyPrefix)
	if raw.KeyPrefix[(len(raw.KeyPrefix)-1):] != "/" {
		bufferKeyPrefix.WriteString("/")
		self.KeyName = "/"
	}
	h, _ := os.Hostname()
	bufferKeyPrefix.WriteString(h)
	self.KeyName = bufferKeyPrefix.String()
	if self.KeyName == "" {
		return fmt.Errorf("Wrong etcd key name.\n")
	}

	self.EtcdKeyTTL, err = time.ParseDuration(raw.EtcdKeyTTL)
	if err != nil {
		return fmt.Errorf("EtcdKeyTTL: %v", err)
	}

	self.EtcdKeyFreq, err = time.ParseDuration(raw.EtcdKeyFreq)
	if err != nil {
		return fmt.Errorf("EtcdKeyFreq: %v", err)
	}

	self.EtcdTimeout, err = time.ParseDuration(raw.EtcdTimeout)
	if err != nil {
		return fmt.Errorf("EtcdTimeout: %v", err)
	}
	self.TTLOnExit, err = time.ParseDuration(raw.TTLOnExit)
	if err != nil {
		return fmt.Errorf("TTLOnExit: %v", err)
	}
	self.CheckTimeout, err = time.ParseDuration(raw.CheckTimeout)
	if err != nil {
		return fmt.Errorf("CheckTimeout: %v", err)
	}

	if raw.ExposeIPaddr != "" {
		self.exposeIPaddr = raw.ExposeIPaddr
	} else {
		var ifs []net.Interface
		ifs, err = net.Interfaces()
		if err != nil {
			return fmt.Errorf("Getting intefaces: %v", err)
		}
		for i := range ifs {
			if strings.Compare(ifs[i].Name, raw.ExposeIface) == 0 {
				var addrs []net.Addr
				addrs, err = ifs[i].Addrs()
				if err != nil {
					return fmt.Errorf("Getting IPs list: %v", err)
				}
				for an := range addrs {
					var s net.IP
					s, _, err = net.ParseCIDR(addrs[an].String())
					if err != nil {
						continue
					}
					r := net.IP.To4(s.To4())
					if r != nil {
						self.exposeIPaddr = r.String()
						break
					}
				}
			}
		}
	}

	if self.exposeIPaddr == "" {
		return fmt.Errorf("Couldn't find exposed IP.\n")
	}

	var bufferWriteActive bytes.Buffer
	bufferWriteActive.WriteString("{\"host\": \"")
	bufferWriteActive.WriteString(self.exposeIPaddr)
	bufferWriteActive.WriteString("\", \"ttl\": 1, \"port\": ")
	bufferWriteActive.WriteString(raw.ExposePort)
	bufferWriteActive.WriteString(", \"weight\": ")
	bufferWriteActive.WriteString(raw.WeightActive)
	bufferWriteActive.WriteString(" }")
	self.writeActive = bufferWriteActive.String()

	if self.KeyName == "" {
		return fmt.Errorf("Wrong sting to write when active.\n")
	}

	var bufferWriteExiting bytes.Buffer
	bufferWriteExiting.WriteString("{\"host\": \"")
	bufferWriteExiting.WriteString(self.exposeIPaddr)
	bufferWriteExiting.WriteString("\", \"ttl\": 1, \"port\": ")
	bufferWriteExiting.WriteString(raw.ExposePort)
	bufferWriteExiting.WriteString(", \"weight\": 0 }")
	self.writeExiting = bufferWriteExiting.String()

	if self.KeyName == "" {
		return fmt.Errorf("Wrong sting to write when exitting.\n")
	}

	if raw.CheckTCP != "" {
		self.Check = self.checkTCP
		self.checkParams = raw.CheckTCP
	} else if raw.CheckHTTP != "" {
		self.Check = self.checkHTTP
		self.checkParams = raw.CheckHTTP

		self.CheckHTTPCode, err = strconv.Atoi(raw.CheckHTTPCode)
		if err != nil {
			return fmt.Errorf("CheckHTTPCode: %v", err)
		}
	}

	if self.Check == nil {
		return fmt.Errorf("Does not set check method.\n")
	}

	return nil
}

func Load(configfile string)(map[string]*Service, error) {
	var s_config map[string]rawService
	_, err := toml.DecodeFile(configfile, &s_config)
	if err != nil {
		return nil, err
	}

	services := make(map[string]*Service, len(s_config))

	for s := range s_config {
		var temp Service
		err := temp.CreateFrom(s_config[s])
		if err != nil {
			return nil, err
		}
		services[s] = &temp
	}

	return services, nil
}

func (self *Service) Start() {
	var etcdClient client.Client
	config := client.Config{Endpoints: self.EtcdServer}
	etcdClient, _ = client.New(config)
	self.keysApi = client.NewKeysAPI(etcdClient)

	self.SigChan = make(chan os.Signal, 1)
	self.ExitChan = make(chan bool, 1)
	signal.Notify(self.SigChan, syscall.SIGINT)
	signal.Notify(self.SigChan, syscall.SIGTERM)

	if !self.Check() {
		err := self.WriteActive()
		if err != nil {
			fmt.Printf("%v\n", err)
		}
	}

	go self.Loop()
}

func (self *Service) WriteActive() error {
	options := client.SetOptions{TTL: self.EtcdKeyTTL}
	ctx, cancel := context.WithTimeout(context.Background(), self.EtcdTimeout)
	_, err := self.keysApi.Set(ctx, self.KeyName, self.writeActive, &options)
	cancel()

	return err
}

func (self *Service) WriteExiting() error {
	options := client.SetOptions{TTL: self.TTLOnExit}
	ctx, cancel := context.WithTimeout(context.Background(), self.EtcdTimeout)
	_, err := self.keysApi.Set(ctx, self.KeyName, self.writeExiting, &options)
	cancel()

	return err
}

func (self *Service) RefreshActive() error {
	options := client.SetOptions{TTL: self.EtcdKeyTTL, Refresh: true}
	ctx, cancel := context.WithTimeout(context.Background(), self.EtcdTimeout)
	_, err := self.keysApi.Set(ctx, self.KeyName, "", &options)
	cancel()

	return err
}

func (self *Service) Delete() error {
	options := client.DeleteOptions{}
	ctx, cancel := context.WithTimeout(context.Background(), self.EtcdTimeout)
	_, err := self.keysApi.Delete(ctx, self.KeyName, &options)
	cancel()

	return err
}

func (self *Service) Loop() {
	LOOP:
	for {
		if !self.Check() {
			select {
			case x := <-self.SigChan:
				fmt.Printf("Child %v: got signal %v\n", self.KeyName, x)

				err := self.WriteExiting()

				if err != nil {
					fmt.Printf("%v\n", err)
				}
				self.ExitChan <- true
				break LOOP
			default:
				err := self.RefreshActive()

				if err != nil {
					if strings.HasPrefix(err.Error(), "100: Key not found") {
						fmt.Printf("After RefreshActive: %v. Trying WriteActive\n", err)

						err := self.WriteActive()
						if err != nil {
							fmt.Printf("After RefreshActive, WriteActive: %v.\n", err)
						}
					}
				}
			}
		} else {
			err := self.Delete()

			if err != nil {
				if ! strings.HasPrefix(err.Error(), "100: Key not found") {
					fmt.Printf("%v\n", err)
				}
			} else {
				fmt.Printf("Deleted %s\n", self.KeyName)
			}
			select {
			case x := <-self.SigChan:
				fmt.Printf("Child %v: got signal %v\n", self.KeyName, x)
				self.ExitChan <- true
				break LOOP
			default:
			}
		}

		time.Sleep(self.EtcdKeyFreq)
	}

	return
}
