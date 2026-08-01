package cron

type Cron struct{}

func (*Cron) AddFunc(string, func()) {}
