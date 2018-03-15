package model

import (
        "crypto/rand"
        "encoding/base32"
        "strings"
)

func newId() string {
        bytes := make([]byte, 16)
        rand.Read(bytes)
        str := base32.StdEncoding.EncodeToString(bytes)
        return strings.TrimSuffix(str, "======")
}
