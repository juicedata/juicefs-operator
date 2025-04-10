/*
 * Copyright 2024 Juicedata Inc
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package app

import (
	"flag"
	"os"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/juicedata/juicefs-operator/internal/controller"
	"github.com/juicedata/juicefs-operator/pkg/common"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	// +kubebuilder:scaffold:imports
)

var setupLog = ctrl.Log.WithName("setup")

var ControllerCmd = &cobra.Command{
	Use: "controller",
	Run: func(cmd *cobra.Command, args []string) {
		run()
	},
}

var zapOpts = zap.Options{
	Development: true,
}

func init() {
	fs := flag.NewFlagSet("", flag.ExitOnError)
	zapOpts.BindFlags(fs)
	ControllerCmd.Flags().AddGoFlagSet(fs)
	ControllerCmd.PersistentFlags().IntVarP(&common.MaxSyncConcurrentReconciles, "max-sync-concurrent-reconciles", "", 10, "max concurrent reconciles for sync")
	ControllerCmd.PersistentFlags().Float32VarP(&common.K8sClientQPS, "k8s-client-qps", "", 30, "QPS indicates the maximum QPS to the master from this client. Setting this to a negative value will disable client-side ratelimiting")
	ControllerCmd.PersistentFlags().IntVarP(&common.K8sClientBurst, "k8s-client-burst", "", 20, "Maximum burst for throttle")
}

func run() {
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))
	mgr, err := NewManager()
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}
	if err := (&controller.CacheGroupReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CacheGroup")
		os.Exit(1)
	}

	if err := (&controller.WarmUpReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create warmup controller", "controller", "WarmUp")
		os.Exit(1)
	}

	if err := (&controller.SyncReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create sync controller", "controller", "Sync")
		os.Exit(1)
	}

	if err := (&controller.CronSyncReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create cron sync controller", "controller", "Sync")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
