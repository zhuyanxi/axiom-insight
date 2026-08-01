module example.com/p0-cron

go 1.26.1

require github.com/robfig/cron/v3 v3.0.0

replace github.com/robfig/cron/v3 => ./stubs/cron
