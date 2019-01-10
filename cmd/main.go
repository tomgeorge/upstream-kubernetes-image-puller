//
// Copyright (c) 2019 Red Hat, Inc.
// This program and the accompanying materials are made
// available under the terms of the Eclipse Public License 2.0
// which is available at https://www.eclipse.org/legal/epl-2.0/
//
// SPDX-License-Identifier: EPL-2.0
//
// Contributors:
//   Red Hat, Inc. - initial API and implementation
//

package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	conf "github.com/redhat-developer/kubernetes-image-puller/internal/configuration"
	"github.com/redhat-developer/kubernetes-image-puller/internal/utils"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func cacheImagesForUser(impersonateUser string,
	config *rest.Config,
	shutdownChan chan os.Signal,
	wg *sync.WaitGroup) {

	log.Printf("Starting caching process for impersonate user %s", impersonateUser)
	config.Impersonate = rest.ImpersonationConfig{
		UserName: impersonateUser,
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf(err.Error())
	}

	// Clean up existing deployment if necessary
	utils.DeleteDaemonsetIfExists(clientset)
	// Create daemonset to cache images
	utils.CacheImages(clientset)

	for {
		select {
		case <-shutdownChan:
			log.Printf("Received SIGTERM, deleting daemonset")
			utils.DeleteDaemonsetIfExists(clientset)
			wg.Done()
		case <-time.After(time.Duration(conf.Config.CachingInterval) * time.Hour):
			utils.EnsureDaemonsetExists(clientset)
		}
	}
}

func main() {
	log.SetOutput(os.Stdout)

	// Set up kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf(err.Error())
	}

	// Shared config to use osoproxy
	config.BearerToken = utils.GetServiceAccountToken(
		conf.Config.ServiceAccountID,
		conf.Config.ServiceAccountSecret,
		conf.Config.OidcProvider,
	)
	config.Host = conf.Config.ProxyURL
	config.TLSClientConfig = rest.TLSClientConfig{
		Insecure: true,
	}

	var wg sync.WaitGroup
	wg.Add(len(conf.Config.ImpersonateUsers))
	for _, user := range conf.Config.ImpersonateUsers {
		var shutdownChan = make(chan os.Signal, 1)
		signal.Notify(shutdownChan, syscall.SIGTERM)

		configCopy := *config
		go cacheImagesForUser(user, &configCopy, shutdownChan, &wg)
	}
	wg.Wait()
	log.Printf("Shutting down cleanly")
}
