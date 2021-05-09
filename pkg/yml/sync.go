package yml

import (
	"context"
	"github.com/helmwave/helmwave/pkg/feature"
	"github.com/helmwave/helmwave/pkg/kubedog"
	"github.com/helmwave/helmwave/pkg/parallel"
	"github.com/helmwave/helmwave/pkg/release"
	"github.com/helmwave/helmwave/pkg/repo"
	log "github.com/sirupsen/logrus"
	"github.com/werf/kubedog/pkg/kube"
	"github.com/werf/kubedog/pkg/tracker"
	"github.com/werf/kubedog/pkg/trackers/rollout/multitrack"
	helm "helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"time"
)

func (c *Config) SyncRepos(settings *helm.EnvSettings) error {
	return repo.Sync(c.Repositories, settings)
}

func (c *Config) SyncReleases(manifestPath string) error {
	return release.Sync(c.Releases, manifestPath)
}

func (c *Config) Sync(manifestPath string, settings *helm.EnvSettings) (err error) {
	err = c.SyncRepos(settings)
	if err != nil {
		return err
	}

	return c.SyncReleases(manifestPath)
}

func (c *Config) SyncFake(manifestPath string, settings *helm.EnvSettings) error {
	// Force disable dependencies during fake deploy
	// and restore setting later
	deps := feature.Dependencies
	feature.Dependencies = false
	defer func(deps bool) {
		feature.Dependencies = deps
	}(deps)

	log.Info("🛫 Fake deploy")
	for i := range c.Releases {
		c.Releases[i].Options.DryRun = true
	}
	return c.Sync(manifestPath, settings)
}

func (c *Config) SyncWithKubedog(manifestPath string, settings *helm.EnvSettings, kubedogConfig *kubedog.Config) error {
	err := c.SyncFake(manifestPath, settings)
	if err != nil {
		return err
	}
	log.Debug("🛫 Fake deploy has been finished")

	mapSpecs, err := release.MakeMapSpecs(c.Releases, manifestPath)
	if err != nil {
		return err
	}

	wg := parallel.NewWaitGroup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = c.runMultitracks(ctx, mapSpecs, settings, kubedogConfig, wg)
	if err != nil {
		return err
	}

	wg.Add(1)
	go func(c *Config, manifestPath string, wg *parallel.WaitGroup, cancel context.CancelFunc) {
		defer wg.Done()
		defer cancel()
		wg.ErrChan() <- c.SyncReleases(manifestPath)
	}(c, manifestPath, wg, cancel)

	return wg.Wait()
}

func (c *Config) runMultitracks(parentContext context.Context, mapSpecs map[string]*multitrack.MultitrackSpecs, settings *helm.EnvSettings, kubedogConfig *kubedog.Config, wg *parallel.WaitGroup) error {
	opts := multitrack.MultitrackOptions{
		StatusProgressPeriod: kubedogConfig.StatusInterval,
		Options: tracker.Options{
			ParentContext: parentContext,
			Timeout:       kubedogConfig.Timeout,
			LogsFromTime:  time.Now(),
		},
	}

	for ns, specs := range mapSpecs {
		log.Info("🐶 kubedog for ", ns)
		// Needs to testing with several  ns
		err := kube.Init(kube.InitOptions{})
		if err != nil {
			return err
		}
		kube.Context = settings.KubeContext
		kube.DefaultNamespace = ns

		kubeClient := kube.Client

		go func(delay time.Duration, kubeClient kubernetes.Interface, specs multitrack.MultitrackSpecs, opts multitrack.MultitrackOptions, wg *parallel.WaitGroup) {
			defer wg.Done()
			time.Sleep(delay)
			wg.Add(1)

			wg.ErrChan() <- multitrack.Multitrack(kubeClient, specs, opts)
		}(kubedogConfig.StartDelay, kubeClient, *specs, opts, wg)
	}
	return nil
}

func (c *Config) Status(releases []string) error {
	return release.Status(c.Releases, releases)
}

func (c *Config) ListReleases() error {
	return release.List(c.Releases)
}
