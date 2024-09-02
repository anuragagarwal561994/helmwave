package helper

import (
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/version"

	log "github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/action"
	helm "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
)

var (
	// Helm is an instance of helm CLI.
	Helm = helm.New()

	// Default logLevel for helm logs.
	helmLogLevel = log.Debugf

	// HelmRegistryClient  is an instance of helm registry client.
	HelmRegistryClient *registry.Client
)

func init() {
	var err error
	HelmRegistryClient, err = registry.NewClient(
		registry.ClientOptDebug(Helm.Debug),
		registry.ClientOptWriter(log.StandardLogger().Writer()),
		registry.ClientOptCredentialsFile(Helm.RegistryConfig),
	)
	if err != nil {
		log.Fatal(err)
	}
}

func wrapConfigFn(client *rest.Config) *rest.Config {
	client.QPS = 100   // default is 5.0
	client.Burst = 100 // default is 10

	return client
}

// NewCfg creates helm internal configuration for provided namespace and kubecontext.
func NewCfg(ns, kubecontext string) (*action.Configuration, error) {
	cfg := new(action.Configuration)
	helmDriver := os.Getenv("HELM_DRIVER") // TODO: get rid of getenv in runtime
	config := genericclioptions.NewConfigFlags(true)
	config.WrapConfigFn = wrapConfigFn
	config.Namespace = &ns
	config.Context = &kubecontext

	if Helm.KubeAPIServer != "" {
		config.APIServer = &Helm.KubeAPIServer
	}

	if Helm.KubeToken != "" {
		config.BearerToken = &Helm.KubeToken
	}

	config.Insecure = &Helm.KubeInsecureSkipTLSVerify

	if Helm.Debug {
		helmLogLevel = log.Infof
	}
	err := cfg.Init(config, ns, helmDriver, helmLogLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to create helm configuration for %s namespace: %w", ns, err)
	}

	cfg.RegistryClient = HelmRegistryClient

	return cfg, nil
}

// NewHelm is a hack to create an instance of helm CLI and specifying namespace without environment variables.
func NewHelm(ns string) *helm.EnvSettings {
	env := helm.New()
	env.SetNamespace(ns)

	return env
}

// GetKubernetesVersion returns kubernetes server version.
//
//nolint:wrapcheck
func GetKubernetesVersion(cfg *action.Configuration) (*version.Info, error) {
	clientSet, err := cfg.KubernetesClientSet()
	if err != nil {
		return nil, err
	}

	return clientSet.Discovery().ServerVersion()
}
