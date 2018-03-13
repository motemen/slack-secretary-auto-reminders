package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/lestrrat-go/slack"
	"github.com/lestrrat-go/slack/rtm"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

type config struct {
	Rules []rule `yaml:"rules"`
}

type rule struct {
	Pattern     regexpString  `yaml:"pattern"`
	RemindAfter time.Duration `yaml:"remind_after"`
}

type regexpString struct {
	*regexp.Regexp
}

// UnmarshalYAML implements yaml.Unmarshaler
func (r *regexpString) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	err := unmarshal(&s)
	if err != nil {
		return err
	}

	r.Regexp, err = regexp.Compile(s)
	if err != nil {
		return errors.Wrapf(err, "%q", s)
	}

	return nil
}

var configFile = "config.yaml"

func main() {
	b, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatalf("could not read %s: %v", configFile, err)
	}

	var conf config
	err = yaml.Unmarshal(b, &conf)
	if err != nil {
		log.Fatalf("could not parse %s: %v", configFile, err)
	}

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cli := slack.New(os.Getenv("SLACK_TOKEN"))
	auth, err := cli.Auth().Test().Do(ctx)
	if err != nil {
		log.Fatalf("Cannot test authentication: %v", err)
	}

	log.Printf("authenticated as: %s", auth.User)

	rtmCli := rtm.New(cli)
	go rtmCli.Run(ctx)

	for e := range rtmCli.Events() {
		if e.Type() != rtm.MessageType {
			continue
		}

		msg := e.Data().(*rtm.MessageEvent)
		if msg.User != auth.UserID {
			continue
		}

		for _, r := range conf.Rules {
			if !r.Pattern.MatchString(msg.Text) {
				continue
			}

			perm, err := cli.Chat().GetPermalink(msg.Channel, msg.Timestamp).Do(ctx)
			if err != nil {
				log.Printf("failed to get permalink: %v", err)
				continue
			}

			channelURL := perm.Permalink
			channelURL = channelURL[:strings.LastIndex(channelURL, "/")]

			text := fmt.Sprintf("<%s|%s> in <%s|%s>", perm.Permalink, msg.Text, channelURL, msg.Channel)
			_, err = cli.Reminders().Add(text, int(time.Now().Add(r.RemindAfter).Unix())).Do(ctx)
			if err != nil {
				log.Printf("failed to set reminder: %v", err)
				continue
			}

			log.Printf("set reminder after %s: %q", r.RemindAfter, msg.Text)

			cli.Chat().PostEphemeral(msg.Channel, fmt.Sprintf("set reminder after %s", r.RemindAfter), msg.User).Do(ctx)
		}
	}
}
