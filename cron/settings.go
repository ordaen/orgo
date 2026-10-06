package cron

import (
	"log"

	"github.com/ordaen/orgo/settings"
)

func init() {
	// the settings are loaded on connect
	if err := settings.Register("cron", Settings); err != nil {
		log.Println("Error registering cron settings", err)
	}
}

// Settings of the cron jobs, stored in the settings table with the key "cron"
var Settings = &CronSettings{
	KeepLogRecords: 30,
}

// CronSettings are the settings of the cron jobs, see Settings.
type CronSettings struct {
	// KeepLogRecords is the number of the not protected logs kept for a job, <= 0 keeps all
	KeepLogRecords int `json:"keep_log_records"`
}

// keepLogRecords returns KeepLogRecords, read safely while the settings are updated.
func keepLogRecords() int {
	if s, ok := settings.Get[CronSettings]("cron"); ok {
		return s.KeepLogRecords
	}
	return 0
}
