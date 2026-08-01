package httpfixture

import "net/http"

func Orders(http.ResponseWriter, *http.Request) {}

func Register(mux *http.ServeMux) {
	http.HandleFunc("GET /orders", Orders)
	mux.Handle("/users", http.HandlerFunc(Orders))
}
