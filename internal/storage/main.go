package storage

import (
	"log"
	"terrapak/internal/config/mid"
	"terrapak/internal/storage/providers/s3"
)

type StorageProvider interface {
	Type() string
	Download(mid mid.MID) (url string, err error)
	Upload(mid mid.MID, data []byte) error
	Delete(mid mid.MID) error
}


func NewClient(protocol string) StorageProvider {
	
	switch protocol {
		case "s3":
			return s3.NewProvider()
		default:
			log.Fatal("invalid protocol")
		}

	return nil
}


