package gcs

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"

	m "github.com/elafros/elafros/sample/autoscale/kdt3/model"
)

var resourcesUrl string

func init() {
	resourcesUrl = os.Getenv("RESOURCES_URL")
}

func Load(gameId string) (*m.Game, error) {
	g := &m.Game{}
	var raw []byte
	if resourcesUrl == "" {
		r, err := ioutil.ReadFile("resources/game.json")
		if err != nil {
			return nil, err
		}
		raw = r
	} else {
		res, err := http.Get(resourcesUrl + "game.json")
		if err != nil {
			return nil, err
		}
		r, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		raw = r
	}
	err := json.Unmarshal(raw, g)
	if err != nil {
		return nil, err
	}
	return g, nil
}
