module example.com/p0-kafka

go 1.26.1

require github.com/IBM/sarama v0.0.0

replace github.com/IBM/sarama => ./stubs/sarama
