package httpclientfixture

import (
	"net/http"
)

func Run(client *http.Client) {
	http.Get("https://orders.example.test/orders")
	client.Get("https://users.example.test/users")
	request, _ := http.NewRequest("POST", "https://billing.example.test/payments", nil)
	client.Do(request)
}
