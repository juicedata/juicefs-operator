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

package utils

import (
	"time"

	v1 "k8s.io/api/batch/v1"
)

func GetFinishedJobCondition(job *v1.Job) *v1.JobCondition {
	// find the job final status condition. if job is resumed, the first condition type is 'Suspended'
	for _, condition := range job.Status.Conditions {
		// job is finished.
		if condition.Type == v1.JobFailed || condition.Type == v1.JobComplete {
			return &condition
		}
	}
	return nil
}

func CalculateDuration(creationTime time.Time, finishTime time.Time) string {
	if finishTime.IsZero() {
		finishTime = time.Now()
	}
	return finishTime.Sub(creationTime).Round(time.Second).String()
}
