package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/goburrow/modbus"
	"github.com/squash/simplestack"
)

type node struct {
	Address      string
	Label        string
	stack        *simplestack.Stack
	CurrentValue uint16
	Average      uint16
}

type config struct {
	Listen        string
	Port          string
	Device        uint
	Baud          int
	Retries       uint
	Nodes         []node
	Interval      int
	AverageWindow int
	lock          sync.Mutex
	OpenTSDB      string
}

// getUint16FromString will parse a hex or decimal value to a uint16
func getUint16FromString(in string) uint16 {
	var a uint16
	if strings.HasPrefix(in, "0x") {
		tmp := in[2:]
		b, err := strconv.ParseUint(tmp, 16, 16)
		if err != nil {
			log.Fatal(err)
		}
		a = uint16(b)
	} else {
		b, err := strconv.ParseUint(in, 10, 16)
		if err != nil {
			log.Fatal(err)
		}
		a = uint16(b)
	}
	return a
}

// readRegister actually pulls data via the serial modbus connection and outputs as it goes unless we're using json
func readRegister(client modbus.Client, a string) (uint16, error) {
	r := getUint16FromString(a)
	v, err := client.ReadHoldingRegisters(r, 1)
	if err != nil {
		if err.Error() == "serial: timeout" {
			return r, errors.New("Timeout")
		}
		return 0, err
	}
	tmp := binary.BigEndian.Uint16(v[:])
	return tmp, nil
}

func main() {
	var c config
	configfile := ""
	flag.StringVar(&configfile, "config", "./modbus-poller.conf", "Specify config file (default is ./modbus-http.conf)")
	flag.Parse()

	cf, err := os.ReadFile(configfile)
	if err != nil {
		log.Fatalf("Couldn't open config file %s: %s", configfile, err.Error())
	}
	err = json.Unmarshal(cf, &c)
	if err != nil {
		log.Fatalf("Error parsing config file: %s", err.Error())
	}

	// Set up our values
	for x := range c.Nodes {
		tmp := c.Nodes[x]
		tmp.stack = simplestack.NewStack(c.AverageWindow)
		c.Nodes[x] = tmp
	}
	if c.Listen != "" {
		go poller(&c)
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			c.lock.Lock()
			j, err := json.Marshal(c.Nodes)
			c.lock.Unlock()
			if err != nil {
				log.Fatal(err)
			}
			w.Write(j)
		})
		log.Fatal(http.ListenAndServe(c.Listen, nil))
	} else {
		poller(&c)
	}

}

func poll(c *config) {
	c.lock.Lock()
	defer c.lock.Unlock()
	locker := flock.New(c.Port)
	locked, err := locker.TryLock()
	if !locked {
		log.Println(err.Error())
		return
	}
	defer locker.Unlock()
	handler := modbus.NewRTUClientHandler(c.Port)
	handler.BaudRate = c.Baud
	handler.DataBits = 8
	handler.Parity = "N"
	handler.StopBits = 1
	handler.SlaveId = byte(c.Device)
	handler.Timeout = 5 * time.Second
	var metrics []string
	err = handler.Connect()
	if err != nil {
		log.Fatal(err)
	}
	failed := false
	client := modbus.NewClient(handler)
	for x := range c.Nodes {
		if !failed {
			node := c.Nodes[x]
			r, err := readRegister(client, c.Nodes[x].Address)
			if err != nil {
				if err.Error() == "serial: timeout" {
					failed = true
					continue
				}
				log.Println(err)
			} else {
				time.Sleep(10 * time.Millisecond) // modbus serial seems to need a rest between queries
			}
			node.CurrentValue = r
			node.stack.Push(r)
			count := 0
			sum := 0
			node.stack.VisitAll(func(i interface{}) {
				tmp := i.(uint16)
				sum = sum + int(tmp)
				count++
			})
			node.Average = uint16(math.Round(float64(sum / count)))
			c.Nodes[x] = node
			if len(c.OpenTSDB) > 0 {
				metrics = append(metrics, fmt.Sprintf("{\"metric\":\"%s\",\"value\":%d}", node.Label, r))
			}
		}
	}
	if len(c.OpenTSDB) > 0 {
		client := &http.Client{
			Timeout: 1 * time.Second,
		}
		body := "[" + strings.Join(metrics, ",") + "]"
		log.Println(body)
		req, err := http.NewRequest(http.MethodPut, c.OpenTSDB, bytes.NewBuffer([]byte(body)))
		if err != nil {
			log.Fatal("Error making new request: " + err.Error())
		}
		_, err = client.Do(req)
		if err != nil {
			log.Printf("Error sending to collector: %s", err.Error())
		}
	}
	c.lock.Unlock()
	handler.Close()
}

func poller(c *config) {
	for {
		go poll(c)
		time.Sleep(time.Duration(c.Interval) * time.Second)
	}

}
