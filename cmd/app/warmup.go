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
	"os"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/juicedata/juicefs-cache-group-operator/internal/controller"
)

var wuSetupLog = ctrl.Log.WithName("wu-setup")

var WarmupCmd = &cobra.Command{
	Use: "warmup-controller",
	Run: func(cmd *cobra.Command, args []string) {
		runWarmupController()
	},
}

func runWarmupController() {
	opts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := NewManager()
	if err != nil {
		wuSetupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	if err := (&controller.WarmUpReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		wuSetupLog.Error(err, "unable to create warmup controller", "controller", "Warmup")
		os.Exit(1)
	}

	wuSetupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		wuSetupLog.Error(err, "running manager error")
		os.Exit(1)
	}
}
