package main

import (
	"encoding/json"
	"fmt"
	"github.com/hpcloud/tail"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

type TelegramResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

func SendMessage(chat, token, message string) error {
	if chat == "" || token == "" {
		// If no configuration provided, don't do anything
		return nil
	}

	endpoint := "https://api.telegram.org/bot" + token + "/sendMessage"

	params := url.Values{}
	params.Set("chat_id", chat)
	params.Set("text", message)

	c := http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(endpoint + "?" + params.Encode())
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	tgResp := &TelegramResponse{}
	err = json.Unmarshal(data, tgResp)

	if err != nil {
		return err
	}

	if !tgResp.OK {
		return fmt.Errorf("telegram returned an error: %v", tgResp.Description)
	}

	return nil
}

func TryGeoIP(ip string) string {
	// If `geoiplookup` is not available, don't attempt to call it
	_, err := exec.LookPath("geoiplookup")
	if err != nil {
		return ""
	}

	cmd := exec.Command("geoiplookup", ip)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	outStr := string(out)
	outStr = strings.Trim(outStr, "\n")
	_, country, _ := strings.Cut(outStr, ": ")

	if strings.HasPrefix(country, "can't") || strings.HasPrefix(country, "IP") {
		return ""
	}

	return country
}

func main() {
	telegramChat := os.Getenv("TELEGRAM_CHAT")
	telegramToken := os.Getenv("TELEGRAM_TOKEN")

	logTail, err := tail.TailFile("/var/log/auth.log", tail.Config{
		ReOpen: true,
		Follow: true,
	})
	if err != nil {
		log.Fatalln(err)
	}

	skipping := true
	now := time.Now()

	for line := range logTail.Lines {
		entryFields := strings.SplitN(line.Text, ": ", 2)
		if len(entryFields) != 2 {
			continue
		}

		meta, text := entryFields[0], entryFields[1]
		metaFields := strings.Fields(meta)
		if len(metaFields) != 5 {
			continue
		}

		date := strings.Join(metaFields[:3], " ")
		hostname := metaFields[3]
		unit := metaFields[4]

		t, err := time.Parse("Jan 2 15:04:05", date)
		if err != nil {
			log.Println("cannot parse time:", err)
			continue
		}

		// Skip all records that exist before starting the tail
		if skipping && (t.Day() != now.Day() ||
			t.Month() != now.Month() ||
			t.Hour() < now.Hour() ||
			(t.Hour() == now.Hour() && t.Minute() < now.Minute()) ||
			(t.Hour() == now.Hour() && t.Minute() == now.Minute() && t.Second() <= now.Second())) {
			continue
		}

		skipping = false

		switch {
		case strings.HasPrefix(unit, "sshd"):
			if !strings.HasPrefix(text, "Accepted") {
				continue
			}

			sshFields := strings.Fields(text)
			if len(sshFields) < 6 {
				continue
			}

			method, user, ip := sshFields[1], sshFields[3], sshFields[5]

			country := TryGeoIP(ip)

			if country == "" {
				fmt.Printf("new ssh session for user %v (from %v; using %v)\n", user, ip, method)
				err = SendMessage(telegramChat, telegramToken,
					fmt.Sprintf("new ssh session started by user %v\n\nfrom: %v\nmethod: %v\nhostname: %v",
						user, ip, method, hostname))
			} else {
				fmt.Printf("new ssh session for user %v (from %v; using %v; country %v)\n", user, ip, method, country)
				err = SendMessage(telegramChat, telegramToken,
					fmt.Sprintf("new ssh session started by user %v\n\nfrom: %v\nmethod: %v\nhostname: %v\ncountry: %v\n",
						user, ip, method, hostname, country))
			}
			if err != nil {
				fmt.Printf("could not send message: %v\n", err)
			}
		case strings.HasPrefix(unit, "sudo"):
			text = strings.TrimSpace(text)
			splitSudo := strings.SplitN(text, " : ", 2)
			if len(splitSudo) != 2 {
				continue
			}

			user, rest := splitSudo[0], splitSudo[1]

			// Ignore command continuations. Maybe this can be handled at a later date.
			if strings.Contains(rest, "(command continued)") {
				continue
			}

			failed := strings.Contains(rest, "incorrect password")

			entries := strings.Split(rest, " ; ")
			var asUser, command string

			for _, entry := range entries {
				switch {
				case strings.HasPrefix(entry, "USER="):
					asUser = entry[5:]
				case strings.HasPrefix(entry, "COMMAND="):
					command = entry[8:]
				}
			}

			if !failed {
				fmt.Printf("sudo executed by %v (became %v; for command %v)\n", user, asUser, command)

				err = SendMessage(telegramChat, telegramToken,
					fmt.Sprintf("sudo started by %v\n\ntarget: %v\ncommand: %v\nhostname: %v",
						user, asUser, command, hostname))
				if err != nil {
					fmt.Printf("could not send message: %v\n", err)
				}
			} else {
				fmt.Printf("failed attempt to execute sudo by %v (to become %v; for command %v)\n", user, asUser, command)

				err = SendMessage(telegramChat, telegramToken,
					fmt.Sprintf("failed attempt to start sudo by %v\n\ntarget: %v\ncommand: %v\nhostname: %v",
						user, asUser, command, hostname))
				if err != nil {
					fmt.Printf("could not send message: %v\n", err)
				}
			}
		}
	}
}
