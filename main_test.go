package main

import (
	"encoding/json"
	"fmt"
	"github.com/griesbacher/nagflux/influx"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"
)

const (
	filename = "config.gcfg"
	envInflux = "NAGFLUX_TEST_INFLUX"
	envLivestatus = "NAGFLUX_TEST_LIVESTATUS"
	envSave = "NAGFLUX_TEST_SAVE"
	databaseName = "NAGFLUX_CI_TEST"
	timeout = time.Duration(20) * time.Second
)

var TestData = []struct {
	input  string
	output influx.SeriesStruct
}{
	{`DATATYPE::SERVICEPERFDATA	TIMET::1	HOSTNAME::b	SERVICEDESC::b	SERVICEPERFDATA::C:\ use=1;;;;	SERVICECHECKCOMMAND::usage
`, influx.SeriesStruct{{Columns: []string{"time", "value"}, Name: `b&b&usage&C: use&value`, Values: [][]interface{}{[]interface{}{1000.0, 1.0}}}}},
	//Basic
	{`DATATYPE::SERVICEPERFDATA	TIMET::2	HOSTNAME::a	SERVICEDESC::a	SERVICEPERFDATA::rta=1;;;;	SERVICECHECKCOMMAND::ping
`, influx.SeriesStruct{{Columns: []string{"time", "value"}, Name: `a&a&ping&rta&value`, Values: [][]interface{}{[]interface{}{2000.0, 1.0}}}}},
	//Test nastystrings
}

var OldConfig string
var influxParam string
var livestatusParam string
var save bool
var finished chan bool

func init() {
	finished = make(chan bool)
	influxParam = os.Getenv(envInflux)
	if influxParam == "" {
		influxParam = "http://127.0.0.1:8086"
		fmt.Printf("%s is not set, using default: %s\n", envInflux, influxParam)
	}

	livestatusParam = os.Getenv(envLivestatus)
	if livestatusParam == "" {
		livestatusParam = "127.0.0.1:6557"
		fmt.Printf("%s is not set, using default: %s\n", envLivestatus, livestatusParam)
	}

	if os.Getenv(envSave) == "" {
		save = true
		fmt.Println("Will save the database")
	}
}

func TestEverything(t *testing.T) {
	createConfig()
	dropDatabase()
	go main()
	time.Sleep(time.Duration(1) * time.Second)
	restoreConfig()
	go createTestData()
	go checkDatabase()
	select {
	case <-finished:
	case <-time.After(timeout):
		result, err := getEverything()
		t.Errorf("Expected data was not found in the influxdb within the timerange: %s\nError: %+v\nDatabase:%+v", timeout, err, result)
	}
	quit <- true
	if !save {
		dropDatabase()
	}
}

func createTestData() {
	if err := os.MkdirAll("test/nagios", 0700); err != nil {
		panic(err)
	}
	nagiosSpoolfile := []byte{}
	for _, data := range TestData {
		nagiosSpoolfile = append(nagiosSpoolfile, []byte(data.input)...)
	}
	if err := ioutil.WriteFile("test/nagios/1", nagiosSpoolfile, 0644); err != nil {
		panic(err)
	}
}

func checkDatabase() {
	for {
		time.Sleep(time.Duration(500) * time.Millisecond)
		query, _ := getEverything()
		result := (*query).Results[0]
		hits := 0
		for _, testData := range TestData {
			for _, series := range result.Series {
				if reflect.DeepEqual(series, testData.output[0]) {
					hits++
					fmt.Println("hit")
				}
			}
		}
		if hits == len(TestData) {
			finished <- true
			return
		}
	}
}

func getEverything() (*influx.ShowSeriesResult, error) {
	resp, err := http.Get(influxParam + "/query?db=" + url.QueryEscape(databaseName) + "&q=select%20*%20from%20%2F.*%2F&epoch=ms")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var jsonResult influx.ShowSeriesResult
		json.Unmarshal(body, &jsonResult)
		return &jsonResult, nil
	}
	return nil, fmt.Errorf("Database query(%s) returned: %s", resp.Request.URL, resp.Status)
}

func dropDatabase() {
	http.Get(influxParam + "/query?q=drop%20database%20" + url.QueryEscape(databaseName))
}

func createConfig() {
	old, _ := ioutil.ReadFile(filename)
	OldConfig = string(old)
	config := []byte(fmt.Sprintf(`
[main]
	NagiosSpoolfileFolder = "test/nagios"
	NagiosSpoolfileWorker = 1
	InfluxWorker = 2
	MaxInfluxWorker = 5
	DumpFile = "nagflux.dump"
	NagfluxSpoolfileFolder = "test/nagflux"

[Log]
	LogFile = ""
	MinSeverity = "DEBUG"

[Monitoring]
	WebserverPort = ""

[Influx]
	Version = 0.9
	Address = "%s"
	Arguments = "precision=ms&db=%s"
	CreateDatabaseIfNotExists = true
	NastyString = "\\ "
	NastyStringToReplace = " "

[Grafana]
	FieldSeperator = "&"

[Livestatus]
	Type = "tcp"
	Address = "%s"`, influxParam, databaseName, livestatusParam))
	if err := ioutil.WriteFile(filename, config, 0644); err != nil {
		panic(err)
	}
}

func restoreConfig() {
	if err := ioutil.WriteFile(filename, []byte(OldConfig), 0644); err != nil {
		panic(err)
	}
}