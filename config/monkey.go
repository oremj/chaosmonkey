// Copyright 2016 Netflix, Inc.
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

package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/netflix/chaosmonkey/config/param"
)

// Monkey is is a config implementation backed by viper
type Monkey struct {
	remote bool // if true, there's a remote provider
	v      *viper.Viper
}

func (m *Monkey) setDefaults() {
	m.v.SetDefault(param.Enabled, false)
	m.v.SetDefault(param.Leashed, true)
	m.v.SetDefault(param.ScheduleEnabled, false)
	m.v.SetDefault(param.Accounts, []string{})
	m.v.SetDefault(param.StartHour, 9)
	m.v.SetDefault(param.EndHour, 15)
	m.v.SetDefault(param.TimeZone, "America/Los_Angeles")
	m.v.SetDefault(param.CronPath, "/etc/cron.d/chaosmonkey-daily-terminations")
	m.v.SetDefault(param.TermPath, "/apps/chaosmonkey/chaosmonkey-terminate.sh")
	m.v.SetDefault(param.TermAccount, "root")
	m.v.SetDefault(param.MaxApps, math.MaxInt32)
	m.v.SetDefault(param.Trackers, []string{})
	m.v.SetDefault(param.Decryptor, "")
	m.v.SetDefault(param.OutageChecker, "")

	m.v.SetDefault(param.DatabasePort, 3306)

	m.v.SetDefault(param.SpinnakerEndpoint, "")
	m.v.SetDefault(param.SpinnakerCertificate, "")
	m.v.SetDefault(param.SpinnakerEncryptedPassword, "")
	m.v.SetDefault(param.SpinnakerUser, "")

	m.v.SetDefault(param.DynamicProvider, "")
	m.v.SetDefault(param.DynamicEndpoint, "")
	m.v.SetDefault(param.DynamicPath, "")

}

func (m *Monkey) setupEnvVarReader() {
	// read from environment variables
	m.v.AutomaticEnv()

	// Replace "." with "_" when reading environment variables
	// e.g.: chaosmonkey.enabled -> CHAOSMONKEY_ENABLED
	m.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
}

// Load returns a Monkey config that loads config from a file
func Load(configPaths []string) (*Monkey, error) {
	m := &Monkey{v: viper.New()}

	m.setDefaults()
	m.setupEnvVarReader()

	for _, dir := range configPaths {
		m.v.AddConfigPath(dir)
	}

	m.v.SetConfigType("toml")
	m.v.SetConfigName("chaosmonkey")

	err := m.v.ReadInConfig()
	// It's ok if the config file doesn't exist, but we want to catch any
	// other config-related issues
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to read config file")
		}

		log.Printf("no config file found, proceeding without one")
	}

	err = m.configureRemote()
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Defaults returns a Monkey config that just has the default values set
// it will not load local files or remote ones
func Defaults() *Monkey {
	v := &Monkey{v: viper.New()}
	v.setDefaults()
	return v
}

// NewFromReader returns a Monkey config which parses the initial config
// from a reader. It may load remote if configured to
// Config file must be in toml format
func NewFromReader(in io.Reader) (*Monkey, error) {
	m := &Monkey{v: viper.New()}
	m.setDefaults()
	m.v.SetConfigType("toml")
	err := m.v.ReadConfig(in)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse config")
	}

	err = m.configureRemote()
	if err != nil {
		return nil, err
	}

	return m, nil

}

// configureRemote configures viper for a remote provider if the user has
// specified one
func (m *Monkey) configureRemote() error {
	provider := m.v.GetString(param.DynamicProvider)
	endpoint := m.v.GetString(param.DynamicEndpoint)
	path := m.v.GetString(param.DynamicPath)

	// If the user specified an external provider, use it
	if provider != "" {
		m.remote = true
		m.v.SetConfigType("json")
		err := m.v.AddRemoteProvider(provider, endpoint, path)
		if err != nil {
			return errors.Wrapf(err, "failed viper.AddRemoteProvider(provider=\"%s\", endpoint=\"%s\", path=\"%s\"):", provider, endpoint, path)
		}
	}
	return nil
}

// SetRemoteProvider sets remote configuration parameters.
// These will typically be set by parsing the config files. This method
// exists to facilitate testing
func (m *Monkey) SetRemoteProvider(provider string, endpoint string, path string) error {
	m.v.Set(param.DynamicProvider, provider)
	m.v.Set(param.DynamicEndpoint, endpoint)
	m.v.Set(param.DynamicPath, path)

	return m.configureRemote()
}

// Set overrides the config value. Used for testing
func (m *Monkey) Set(key string, value interface{}) {
	m.v.Set(key, value)
}

// readRemoteConfig retrieves config parameters from a remote source
// If no remote source has been configured, this is a no-op
func (m *Monkey) readRemoteConfig() error {
	if !m.remote {
		return nil
	}
	return m.v.ReadRemoteConfig()
}

// Enabled returns true if Chaos Monkey is enabled
func (m *Monkey) Enabled() (bool, error) {
	return m.getDynamicBool(param.Enabled)
}

// Leashed returns true if Chaos Monkey is leashed
// In leashed mode, Chaos Monkey records terminations but does not actually
// terminate
func (m *Monkey) Leashed() (bool, error) {
	return m.getDynamicBool(param.Leashed)
}

// ScheduleEnabled returns true if Chaos Monkey termination scheduling is enabled
// if false, Chaos Monkey will not generate a termination schedule
func (m *Monkey) ScheduleEnabled() (bool, error) {
	return m.getDynamicBool(param.ScheduleEnabled)
}

func (m *Monkey) getDynamicBool(param string) (bool, error) {
	err := m.readRemoteConfig()
	if err != nil {
		return false, err
	}

	return m.v.GetBool(param), nil
}

// AccountEnabled returns true if Chaos Monkey is enabled for that account
func (m *Monkey) AccountEnabled(account string) (bool, error) {
	accounts, err := m.Accounts()
	if err != nil {
		return false, err
	}

	for _, x := range accounts {
		if account == x {
			return true, nil
		}
	}

	return false, nil
}

// Accounts return a list of accounts where Choas Monkey is enabled
func (m *Monkey) Accounts() ([]string, error) {
	err := m.readRemoteConfig()
	if err != nil {
		return nil, err
	}

	return m.getStringSlice(param.Accounts)
}

// toStrings converts a slice of interfaces to a slice of strings
func toStrings(values []interface{}) ([]string, error) {
	result := make([]string, len(values))
	for i, x := range values {
		x, valid := x.(string)
		if !valid {
			return nil, errors.Errorf("non-string in %v", values)
		}
		result[i] = x
	}
	return result, nil
}

// StartHour (o'clock) is when Chaos
// Monkey starts terminating this value is in [0,23] This is time-zone
// dependent, see the Location method
func (m *Monkey) StartHour() int { return m.v.GetInt(param.StartHour) }

// EndHour (o'clock) is the time after which Chaos Monkey will
// not terminate instances.
// this value is in [0,23]
// This is time-zone dependent, see the Location method
func (m *Monkey) EndHour() int {
	return m.v.GetInt(param.EndHour)
}

// Location returns the time zone of StartHour and EndHour.
// May return an error if time.LoadLocation fails
func (m *Monkey) Location() (*time.Location, error) {
	return time.LoadLocation(m.v.GetString(param.TimeZone))
}

// CronPath returns the path to where Chaos Monkey
// puts the cron job file with daily terminations
func (m *Monkey) CronPath() string {
	return m.v.GetString(param.CronPath)
}

// TermPath returns the path to the executable that
// wraps the chaos monkey binary for terminating instances
func (m *Monkey) TermPath() string {
	return m.v.GetString(param.TermPath)
}

// TermAccount returns the account that cron will use
// to execute the termination command
func (m *Monkey) TermAccount() string {
	return m.v.GetString(param.TermAccount)
}

// MaxApps returns the maximum number of apps to
// examine for termination
func (m *Monkey) MaxApps() int {
	return m.v.GetInt(param.MaxApps)
}

// Trackers returns the names of the backend implementation for
// termination trackers. Used for things like logging and metrics collection
func (m *Monkey) Trackers() ([]string, error) {
	return m.getStringSlice(param.Trackers)
}

// ErrorCounter returns the names of the backend implementions for
// error counters. Intended for monitoring/alerting.
func (m *Monkey) ErrorCounter() string {
	return m.v.GetString(param.ErrorCounter)
}

func (m *Monkey) getStringSlice(key string) ([]string, error) {
	// This could be encoded natively as a list of strings, or as a string that
	// represents a list of strings, so we need to handle both cases
	t := m.v.Get(key)
	if t == nil {
		return nil, fmt.Errorf("%s not specified", param.Accounts)
	}

	switch t := t.(type) {
	default:
		return nil, fmt.Errorf("%s: unexpected type %T", param.Accounts, t)
	case []string: // When set explicitly in code
		return t, nil
	case []interface{}: // When reading from config file
		return toStrings(t)
	case string: // When reading from prana, which uses string encoding
		// Convert to list of strings
		var result []string
		err := json.Unmarshal([]byte(t), &result)
		return result, err
	}
}

// SpinnakerEndpoint returns the spinnaker endpoint
func (m *Monkey) SpinnakerEndpoint() string {
	return m.v.GetString(param.SpinnakerEndpoint)
}

// SpinnakerCertificate retunrs a path to a .p12 file that contains a TLS cert
// for authenticating against Spinnaker
func (m *Monkey) SpinnakerCertificate() string {
	return m.v.GetString(param.SpinnakerCertificate)
}

// SpinnakerEncryptedPassword returns an password that
// is used to decrypt the Spinnaker certificate. The encryption scheme
// is defined by the Decryptor parameter
func (m *Monkey) SpinnakerEncryptedPassword() string {
	return m.v.GetString(param.SpinnakerEncryptedPassword)
}

// SpinnakerUser is sent in the "user" field in the terminateInstances task sent
// to Spinnaker when Spinnaker terminates an instance
func (m *Monkey) SpinnakerUser() string {
	return m.v.GetString(param.SpinnakerUser)
}

// Decryptor returns an interface for decrypting sercrets
func (m *Monkey) Decryptor() string {
	return m.v.GetString(param.Decryptor)
}

// OutageChecker returns an interface for checking if there is an ongoing
// outage
func (m *Monkey) OutageChecker() string {
	return m.v.GetString(param.OutageChecker)
}

// DatabaseHost returns the hostname the database is running on
func (m *Monkey) DatabaseHost() string {
	return m.v.GetString(param.DatabaseHost)
}

// DatabasePort returns the port the database is listening on
func (m *Monkey) DatabasePort() int {
	return m.v.GetInt(param.DatabasePort)
}

// DatabaseUser returns the database user associated with the credentials
func (m *Monkey) DatabaseUser() string {
	return m.v.GetString(param.DatabaseUser)
}

// DatabaseName returns the name of the database that stores the Chaos Monkey
// state
func (m *Monkey) DatabaseName() string {
	return m.v.GetString(param.DatabaseName)
}

// DatabaseEncryptedPassword returns an encrypted version of the database
// credentials
func (m *Monkey) DatabaseEncryptedPassword() string {
	return m.v.GetString(param.DatabaseEncryptedPassword)
}

// BindPFlag binds a specific parameter to a pflag
func (m *Monkey) BindPFlag(parameter string, flag *pflag.Flag) (err error) {
	return m.v.BindPFlag(parameter, flag)
}

// The code below is to provide a mechanism for adding a new remote config
// provider without directly viper. Viper wasn't desinged for this use-case
// so this is a workaround.

// RemoteProvider is a type alias
type RemoteProvider viper.RemoteProvider

// RemoteConfigFactory is the same interface as viper.remoteConfigFactory
// This is a workaround to be able to support backends other than etc/consul
// without modifying viper
type RemoteConfigFactory interface {
	Get(rp RemoteProvider) (io.Reader, error)
	Watch(rp RemoteProvider) (io.Reader, error)
}

type proxy struct {
	factory RemoteConfigFactory
}

func (p proxy) Get(rp viper.RemoteProvider) (io.Reader, error) {
	return p.factory.Get(rp)
}

func (p proxy) Watch(rp viper.RemoteProvider) (io.Reader, error) {
	return p.factory.Watch(rp)
}

// SetRemoteProvider sets viper's remote provider
func SetRemoteProvider(name string, factory RemoteConfigFactory) {
	viper.RemoteConfig = proxy{factory}
	viper.SupportedRemoteProviders = []string{name}
}
