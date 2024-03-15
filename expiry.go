package main

import (
	"os"
	"strconv"
	"time"

	"github.com/boltdb/bolt"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

func ExpiryDoer() {
	for {
		removed := 0
		db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("expiry"))
			c := b.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				expiryTime, err := strconv.ParseInt(string(v), 10, 64)
				if err != nil {
					log.Error().Err(err).Bytes("k", k).Bytes("v", v).Msg("Expiry time could not be parsed")
					continue
				}
				if time.Now().After(time.Unix(expiryTime, 0)) {
					os.Remove(viper.GetString("filedir") + "/" + string(k))
					removed += 1
					c.Delete()
				}
			}
			return nil
		})
		if removed >= 1 {
			log.Info().Int("amt", removed).Msg("Purged based on expiry")
		}
		time.Sleep(5 * time.Second)
	}
}
