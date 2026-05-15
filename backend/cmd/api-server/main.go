package main

import (
    "encoding/json"
    "log"
    "net/http"
)

type Item struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

var items = []Item{
    {ID: 1, Name: "Item 1"},
    {ID: 2, Name: "Item 2"},
}

func main() {
    http.HandleFunc("/health", healthHandler)
    http.HandleFunc("/items", itemsHandler)
    http.HandleFunc("/items/", itemHandler)

    log.Println("Server running on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func itemsHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    
    if r.Method == http.MethodGet {
        json.NewEncoder(w).Encode(items)
        return
    }

    if r.Method == http.MethodPost {
        var item Item
        json.NewDecoder(r.Body).Decode(&item)
        item.ID = len(items) + 1
        items = append(items, item)
        w.WriteHeader(http.StatusCreated)
        json.NewEncoder(w).Encode(item)
        return
    }

    w.WriteHeader(http.StatusMethodNotAllowed)
}

func itemHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    
    if r.Method == http.MethodGet {
        // In a real app, parse the ID from URL
        json.NewEncoder(w).Encode(items[0])
        return
    }

    w.WriteHeader(http.StatusMethodNotAllowed)
}
