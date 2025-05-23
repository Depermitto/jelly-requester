package main

import (
	"log"
	"os"

	"github.com/Depermitto/wnioskodawca-galaretki/bot"
	"github.com/joho/godotenv"
	bolt "go.etcd.io/bbolt"
)

const (
	path  = "data/requests.db"
	token = "TOKEN"
)

func main() {
	_ = os.Mkdir("data", 0700)
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		log.Fatalln("Cannot open database")
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("media"))
		return err
	})
	if err != nil {
		log.Fatalln("Internal server error")
	}

	_ = godotenv.Load()
	token := os.Getenv(token)
	if len(token) == 0 {
		log.Fatalln("No discord bot token supplied")
	}
	bot.Run(token, db)
}
