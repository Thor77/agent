package prometheus

import (
	"context"
	"errors"
	"path"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/agent/pkg/wal"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
)

var (
	instanceStoppedNormallyErr = errors.New("instance shutdown normally")
)

// TODO(rfratto): define prometheus_build_info metric so the existing
// Prometheus Remote Write dashboard mixin can discover the agent

// InstanceConfig is a specific agent that runs within the overall Prometheus
// agent. It has its own set of scrape_configs and remote_write rules.
type InstanceConfig struct {
	Name          string                      `yaml:"name"`
	ScrapeConfigs []*config.ScrapeConfig      `yaml:"scrape_configs,omitempty"`
	RemoteWrite   []*config.RemoteWriteConfig `yaml:"remote_write,omitempty"`
}

// ApplyDefaults applies default configurations to the configuration to all
// values that have not been changed to their non-zero value.
func (c *InstanceConfig) ApplyDefaults(global *config.GlobalConfig) {
	// TODO(rfratto): what other defaults need to be applied?
	for _, sc := range c.ScrapeConfigs {
		if sc.ScrapeInterval == 0 {
			sc.ScrapeInterval = global.ScrapeInterval
		}
		if sc.ScrapeTimeout == 0 {
			if global.ScrapeTimeout > sc.ScrapeInterval {
				sc.ScrapeTimeout = sc.ScrapeInterval
			} else {
				sc.ScrapeTimeout = global.ScrapeTimeout
			}
		}
	}
}

// Validate checks if the InstanceConfig has all required fields filled out.
// This should only be called after ApplyDefaults.
func (c *InstanceConfig) Validate() error {
	// TODO(rfratto): validation
	return nil
}

// instance is an individual metrics collector and remote_writer.
type instance struct {
	cfg       InstanceConfig
	globalCfg config.GlobalConfig
	logger    log.Logger

	walDir string

	cancelScrape context.CancelFunc

	exited  chan bool
	exitErr error
}

// newInstance creates and starts a new instance.
func newInstance(globalCfg config.GlobalConfig, cfg InstanceConfig, walDir string, logger log.Logger) (*instance, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	i := &instance{
		cfg:       cfg,
		globalCfg: globalCfg,
		logger:    log.With(logger, "instance", cfg.Name),
		walDir:    path.Join(walDir, cfg.Name),
		exited:    make(chan bool),
	}

	wstore, err := wal.NewStorage(i.logger, prometheus.DefaultRegisterer, i.walDir)
	if err != nil {
		return nil, err
	}

	go i.run(wstore)
	return i, nil
}

// Err returns the error generated by instance when shut down. If the shutdown
// was intentional (i.e., the user called stop), then Err returns
// instanceStoppedNormallyErr.
func (i *instance) Err() error {
	return i.exitErr
}

func (i *instance) run(wstore *wal.Storage) {
	ctxScrape, cancelScrape := context.WithCancel(context.Background())
	i.cancelScrape = cancelScrape

	discoveryManagerScrape := discovery.NewManager(ctxScrape, log.With(i.logger, "component", "discovery manager scrape"), discovery.Name("scrape"))
	{
		// TODO(rfratto): refactor this to a function?
		c := map[string]sd_config.ServiceDiscoveryConfig{}
		for _, v := range i.cfg.ScrapeConfigs {
			c[v.JobName] = v.ServiceDiscoveryConfig
		}
		discoveryManagerScrape.ApplyConfig(c)
	}

	// TODO(rfratto): tunable flush deadline?
	remoteStore := remote.NewStorage(log.With(i.logger, "component", "remote"), prometheus.DefaultRegisterer, wstore.StartTime, i.walDir, time.Duration(1*time.Minute))
	remoteStore.ApplyConfig(&config.Config{
		GlobalConfig:       i.globalCfg,
		RemoteWriteConfigs: i.cfg.RemoteWrite,
	})

	fanoutStorage := storage.NewFanout(i.logger, wstore, remoteStore)

	scrapeManager := scrape.NewManager(log.With(i.logger, "component", "scrape manager"), fanoutStorage)
	scrapeManager.ApplyConfig(&config.Config{
		GlobalConfig:  i.globalCfg,
		ScrapeConfigs: i.cfg.ScrapeConfigs,
	})

	var g run.Group
	// Prometheus generally runs a Termination handler here, but termination handling
	// is done outside of the instance.
	// TODO(rfratto): anything else we need to do here?
	{
		// Scrape discovery manager
		g.Add(
			func() error {
				err := discoveryManagerScrape.Run()
				level.Info(i.logger).Log("msg", "service discovery manager stopped")
				return err
			},
			func(err error) {
				level.Info(i.logger).Log("msg", "stopping scrape discovery manager...")
				i.cancelScrape()
			},
		)
	}
	{
		// Scrape manager
		g.Add(
			func() error {
				// TODO(rfratto): because the WAL is being created prior to this being called,
				// this will always start with replaying the WAL, even if it's fresh. Is this
				// expected? Do we want to change this?
				err := scrapeManager.Run(discoveryManagerScrape.SyncCh())
				level.Info(i.logger).Log("msg", "scrape manager stopped")
				return err
			},
			func(err error) {
				// TODO(rfratto): is this correct? do we want to stop the fanoutStorage
				// later?
				if err := fanoutStorage.Close(); err != nil {
					level.Error(i.logger).Log("msg", "error stopping storage", "err", err)
				}
				level.Info(i.logger).Log("msg", "stopping scrape manager...")
				scrapeManager.Stop()
			},
		)
	}

	err := g.Run()
	if err != nil {
		level.Error(i.logger).Log("msg", "agent instance stopped with error", "err", err)
	}
	if i.exitErr == nil {
		i.exitErr = err
	}

	close(i.exited)
}

// Stop stops the instance.
func (i *instance) Stop() {
	i.exitErr = instanceStoppedNormallyErr

	// TODO(rfratto): anything else we need to stop here?
	i.cancelScrape()
	<-i.exited
}
