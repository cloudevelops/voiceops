package main

import (
	"fmt"
	"strings"
	"regexp"

	"net/http"
	"os/exec"
	"encoding/json"

	"github.com/hpcloud/tail"
	"github.com/go-redis/redis"
	"github.com/spf13/viper"
)

type Customer struct {
	Name string
	Company string
	Note string
}

func main() {
	loadConfig()
	var si = tail.SeekInfo{Offset: 0, Whence: 2}
	t, _ := tail.TailFile(viper.GetString("queue_log"), tail.Config{Location: &si, Follow: true})

	for line := range t.Lines {
		parseLine(line.Text)
	}
}

func parseLine(str string){
	fields := strings.Split(str, "|")
	msg := ""
	re := regexp.MustCompile("(ENTERQUEUE|COMPLETEAGENT|COMPLETECALLER|ABANDON)")
	match := re.FindString(str);

	switch match {
	case "ENTERQUEUE":
		msg = buildIncomingMessage(fields)
	case "COMPLETEAGENT", "COMPLETECALLER":
		msg = buildCallCompleteMessage(fields)
	case "ABANDON":
		msg = buildCallAbandonedMessage(fields)
	}

	if msg != "" {
		fmt.Println(msg)
		sendMattermost(msg)
	}
}

func buildIncomingMessage(fields []string) string {
	info, data := getCallerInfo(fields[6])
	return "Incoming call from " + info + " in **" + getQueue(fields[2]) + "** queue" + data
}

func buildCallCompleteMessage(fields []string) string {
	url := buildCallUrl(fields)
	phone := getPhoneFromUrl(url)
	info, _ := getCallerInfo(phone)
	return "Recording for call from " + info + " at: " + url
}

func buildCallAbandonedMessage(fields []string) string {
	url := buildCallUrl(fields)
	phone := getPhoneFromUrl(url)
	info, _ := getCallerInfo(phone)
	return "Call from " + info + " has left the **" + getQueue(fields[2]) + "** queue"
}

func sendMattermost(alert string) {
	apiUrl := viper.GetString("mattermost_api_url")
	jsonStr := []byte(`{"text":"` + alert + `"}`)
	r := strings.NewReader(string(jsonStr))

	res, _ := http.Post(apiUrl, "text/plain", r)
	fmt.Println(res)
}

func getQueue(queueCode string) string {
	queue := ""
	switch queueCode {
	case "1000":
		queue = "support"
	case "2000":
		queue = "info"
	case "3000":
		queue = "sales"
	}
	return queue
}

func buildCallUrl(fields []string) string {
	out, err := exec.Command("/usr/bin/find", viper.GetString("recording_path"), "-name",
		"*"+fields[1]+"*").Output()
	if err != nil {
		fmt.Println(err)
	}

	strOut := strings.Replace(string(out), viper.GetString("recording_path"), "", 1)
	fileName := strOut[:len(strOut)-1]

	return viper.GetString("recording_url") + fileName
}

func getPhoneFromUrl(url string) string {
	return strings.Split(url, "-")[2]
}

func getCallerInfo(phone string) (string, string) {
	client := redis.NewClient(&redis.Options{
		Addr:     viper.GetString("redis_host"),
		Password: viper.GetString("redis_password"),
	})

	val := client.Get("e3customers/+420" + phone).Val()
	if val != "" {
		r := strings.NewReplacer("{", "", "}", "", "\"", "", "\n", "\\n", "\r", "")
		jsonVal := r.Replace(val)
		cus := Customer{}
		json.Unmarshal([]byte(val), &cus)
		return "**" + cus.Name + "** (" + cus.Note + ") from **" + cus.Company + "** company",
		"\\n```" + jsonVal + "```"
	} else {
		return "**" + phone + "**", ""
	}
}

func loadConfig() {
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}
}
