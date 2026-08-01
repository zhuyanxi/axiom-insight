package cronfixture

import "github.com/robfig/cron/v3"

func Cleanup() {}

func Register(scheduler *cron.Cron) {
	scheduler.AddFunc("@hourly", Cleanup)
}
