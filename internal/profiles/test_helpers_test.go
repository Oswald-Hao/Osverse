package profiles

func adapterInput() Input {
	return Input{
		ID: "profile", Name: "Work", APIKey: "secret-key-1234",
		BaseURL: "https://api.example/v1", Model: "model-name",
	}
}
