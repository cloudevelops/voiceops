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
	re := regexp.MustCompile("(ENTERQUEUE|COMPLETEAGENT|COMPLETECALLER|ABANDON)")
	match := re.FindString(str);
	fields := strings.Split(str, "|")

	if match != "" && isPhoneNumberAllowed(fields[6]){
		buildMessage(match, fields)
	}
}

func buildMessage(match string, fields []string){
	msg := ""

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

func isPhoneNumberAllowed(phone string) bool {
	whiteList := viper.GetStringSlice("phone_numbers_whitelist")
	if len(whiteList) > 0 {
		for _, b := range whiteList {
			if b == phone {
				return true
			}
		}
		fmt.Println("Ignore phone number: " + phone)
		return false
	} else {
		return true
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
	msg := "Recording for call from " + info + " [here](" + url + ")"
	msg += addNewContactLink(phone)
	return msg
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
	return viper.GetStringMapString("queues")[queueCode]
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

	val := client.Get(viper.GetString("redis_path") + phone).Val()
	if val != "" {
		r := strings.NewReplacer("{", "", "}", "", "\"", "", "\n", "\\n", "\r", "")
		jsonVal := r.Replace(val)
		cus := Customer{}
		json.Unmarshal([]byte(val), &cus)

		return buildCallerInfo(phone, cus), "\\n```" + jsonVal + "```"
	} else {
		return "**" + phone + "**", ""
	}
}

func buildCallerInfo(phone string, customer Customer) string {
	info := "**" + customer.Name + "**, **" + phone + "**"
	if customer.Note != "" {
		info += " (" + customer.Note + ")"
	}
	if customer.Company != "" {
		info += " from **" + customer.Company + "** company"
	}
	return info
}

func loadConfig() {
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}
}

func addNewContactLink(phone string) string {
	if viper.GetBool("show_add_contact_url") {
		return "\\n[Add new customer to DB](" + viper.GetString("add_contact_url") + phone + ")"
	}
	return ""
}
