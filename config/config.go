package config

import (
	"encoding/json"
	"io/ioutil"
	"os"
)

type Config struct {
	ContributionsStoreAddr string `json:"contributionsStoreAddr"`
	ValidationsStoreAddr   string `json:"validationsStoreAddr"`
	PeerID                 string `json:"peerID"`
}

// TODO : store config and cache in appropriate directories
//
// loads the persistent config file into config struct
func LoadConfig() (*Config, error) {
	file, err := os.Open(*FlagRepo + "_config")
	if err != nil {
		if os.IsNotExist(err) {
			// default config in case none was found
			config := &Config{
				ContributionsStoreAddr: "contributions",
				ValidationsStoreAddr:   "validations",
				PeerID:                 "",
			}
			return config, nil
		}
		return nil, err
	}
	defer file.Close()

	data, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}

	config := &Config{}
	err = json.Unmarshal(data, config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

// writes a file in json format from a struct
// TODO : move this to a utils file/package
func SaveStructAsJSON(s interface{}, path string) error {

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(path, data, 0644)
	if err != nil {
		return err
	}

	return nil
}
