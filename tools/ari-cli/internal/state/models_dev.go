package state

type ModelsDevClient struct {
	baseURL string
}

func NewModelsDevClient() *ModelsDevClient {
	return &ModelsDevClient{
		baseURL: "https://models.dev/api.json",
	}
}

func (c *ModelsDevClient) FetchModels() error {
	return nil
}
