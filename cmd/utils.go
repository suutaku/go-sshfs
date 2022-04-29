// Copyright © 2016 Asteris, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"log/syslog"
	"net/url"

	"github.com/rifflock/lfshook"
	"github.com/sirupsen/logrus"
	logrus_syslog "github.com/sirupsen/logrus/hooks/syslog"
	"github.com/spf13/viper"
	"github.com/wercker/journalhook"
	"golang.org/x/sys/unix"
)

func initLogging() {
	// level
	level, err := logrus.ParseLevel(viper.GetString("log-level"))
	if err != nil {
		logrus.WithError(err).Warn(`invalid log level. Defaulting to "info"`)
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)

	// format
	switch viper.GetString("log-format") {
	case "text":
		logrus.SetFormatter(new(logrus.TextFormatter))
	case "json":
		logrus.SetFormatter(new(logrus.JSONFormatter))
	default:
		logrus.SetFormatter(new(logrus.TextFormatter))
		logrus.WithField("format", viper.GetString("log-format")).Warn(`invalid log format. Defaulting to "text"`)
	}

	// output
	dest, err := url.Parse(viper.GetString("log-destination"))
	if err != nil {
		logrus.WithError(err).WithField("destination", viper.GetString("log-destination")).Error(`invalid log destination. Defaulting to "stdout:"`)
		dest.Scheme = "stdout"
	}

	switch dest.Scheme {
	case "stdout":
		// default, we don't need to do anything
	case "file":
		logrus.AddHook(lfshook.NewHook(lfshook.PathMap{
			logrus.DebugLevel: dest.Opaque,
			logrus.InfoLevel:  dest.Opaque,
			logrus.WarnLevel:  dest.Opaque,
			logrus.ErrorLevel: dest.Opaque,
			logrus.FatalLevel: dest.Opaque,
		}, &logrus.JSONFormatter{}))
	case "journald":
		journalhook.Enable()
	case "syslog":
		hook, err := logrus_syslog.NewSyslogHook(dest.Fragment, dest.Host, syslog.LOG_DEBUG, dest.User.String())
		if err != nil {
			logrus.WithError(err).Error("could not configure syslog hook")
		} else {
			logrus.AddHook(hook)
		}
	default:
		logrus.WithField("destination", viper.GetString("log-destination")).Warn(`invalid log destination. Defaulting to "stdout:"`)
	}
}

func lockMemory() {
	err := unix.Mlockall(unix.MCL_FUTURE | unix.MCL_CURRENT)
	switch err {
	case nil:
	case unix.ENOSYS:
		logrus.WithError(err).Warn("mlockall() not implemented on this system")
	case unix.ENOMEM:
		logrus.WithError(err).Warn("mlockall() failed with ENOMEM")
	default:
		logrus.WithError(err).Warn("could not perform mlockall to prevent swapping memory")
	}
}
