/*
Copyright 2020 The Flux authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/fluxcd/flux2/pkg/manifestgen/install"
)

var VERSION = "0.0.0-dev.0"

var rootCmd = &cobra.Command{
	Use:           "flux",
	Version:       VERSION,
	SilenceUsage:  true,
	SilenceErrors: true,
	Short:         "Command line utility for assembling Kubernetes CD pipelines",
	Long: `
Command line utility for assembling Kubernetes CD pipelines the GitOps way.`,
	Example: `  # Check prerequisites
  flux check --pre

  # Install the latest version of Flux
  flux install

  # Create a source for a public Git repository
  flux create source git webapp-latest \
    --url=https://github.com/stefanprodan/podinfo \
    --branch=master \
    --interval=3m

  # List GitRepository sources and their status
  flux get sources git

  # Trigger a GitRepository source reconciliation
  flux reconcile source git flux-system

  # Export GitRepository sources in YAML format
  flux export source git --all > sources.yaml

  # Create a Kustomization for deploying a series of microservices
  flux create kustomization webapp-dev \
    --source=webapp-latest \
    --path="./deploy/webapp/" \
    --prune=true \
    --interval=5m \
    --health-check="Deployment/backend.webapp" \
    --health-check="Deployment/frontend.webapp" \
    --health-check-timeout=2m

  # Trigger a git sync of the Kustomization's source and apply changes
  flux reconcile kustomization webapp-dev --with-source

  # Suspend a Kustomization reconciliation
  flux suspend kustomization webapp-dev

  # Export Kustomizations in YAML format
  flux export kustomization --all > kustomizations.yaml

  # Resume a Kustomization reconciliation
  flux resume kustomization webapp-dev

  # Delete a Kustomization
  flux delete kustomization webapp-dev

  # Delete a GitRepository source
  flux delete source git webapp-latest

  # Uninstall Flux and delete CRDs
  flux uninstall`,
}

var logger = stderrLogger{stderr: os.Stderr}

type rootFlags struct {
	timeout      time.Duration
	verbose      bool
	pollInterval time.Duration
	defaults     install.Options
}

// RequestError is a custom error type that wraps an error returned by the flux api.
type RequestError struct {
	StatusCode int
	Err        error
}

func (r *RequestError) Error() string {
	return r.Err.Error()
}

var rootArgs = NewRootFlags()
var kubeconfigArgs = genericclioptions.NewConfigFlags(false)

func init() {
	rootCmd.PersistentFlags().DurationVar(&rootArgs.timeout, "timeout", 5*time.Minute, "timeout for this operation")
	rootCmd.PersistentFlags().BoolVar(&rootArgs.verbose, "verbose", false, "print generated objects")

	configureDefaultNamespace()
	kubeconfigArgs.APIServer = nil // prevent AddFlags from configuring --server flag
	kubeconfigArgs.Timeout = nil   // prevent AddFlags from configuring --request-timeout flag, we have --timeout instead
	kubeconfigArgs.AddFlags(rootCmd.PersistentFlags())

	// Since some subcommands use the `-s` flag as a short version for `--silent`, we manually configure the server flag
	// without the `-s` short version. While we're no longer on par with kubectl's flags, we maintain backwards compatibility
	// on the CLI interface.
	apiServer := ""
	kubeconfigArgs.APIServer = &apiServer
	rootCmd.PersistentFlags().StringVar(kubeconfigArgs.APIServer, "server", *kubeconfigArgs.APIServer, "The address and port of the Kubernetes API server")

	rootCmd.RegisterFlagCompletionFunc("context", contextsCompletionFunc)
	rootCmd.RegisterFlagCompletionFunc("namespace", resourceNamesCompletionFunc(corev1.SchemeGroupVersion.WithKind("Namespace")))

	rootCmd.DisableAutoGenTag = true
	rootCmd.SetOut(os.Stdout)
}

func NewRootFlags() rootFlags {
	rf := rootFlags{
		pollInterval: 2 * time.Second,
		defaults:     install.MakeDefaultOptions(),
	}
	rf.defaults.Version = "v" + VERSION
	return rf
}

func main() {
	log.SetFlags(0)
	if err := rootCmd.Execute(); err != nil {

		if err, ok := err.(*RequestError); ok {
			if err.StatusCode == 1 {
				logger.Warningf("%v", err)
			} else {
				logger.Failuref("%v", err)
			}

			os.Exit(err.StatusCode)
		}

		logger.Failuref("%v", err)
		os.Exit(1)
	}
}

func configureDefaultNamespace() {
	*kubeconfigArgs.Namespace = rootArgs.defaults.Namespace
	fromEnv := os.Getenv("FLUX_SYSTEM_NAMESPACE")
	if fromEnv != "" {
		kubeconfigArgs.Namespace = &fromEnv
	}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

// readPasswordFromStdin reads a password from stdin and returns the input
// with trailing newline and/or carriage return removed. It also makes sure that terminal
// echoing is turned off if stdin is a terminal.
func readPasswordFromStdin(prompt string) (string, error) {
	var out string
	var err error
	fmt.Fprint(os.Stdout, prompt)
	stdinFD := int(os.Stdin.Fd())
	if term.IsTerminal(stdinFD) {
		var inBytes []byte
		inBytes, err = term.ReadPassword(int(os.Stdin.Fd()))
		out = string(inBytes)
	} else {
		out, err = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	if err != nil {
		return "", fmt.Errorf("could not read from stdin: %w", err)
	}
	fmt.Println()
	return strings.TrimRight(out, "\r\n"), nil
}
