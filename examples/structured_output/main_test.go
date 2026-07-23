package main

import "testing"

// These tests cover the pure JSON parsing helpers only — no network, no
// provider. They guard the fence-stripping and decoding that turn a model
// reply into a typed Recipe.
func TestParseRecipe(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		want    Recipe
	}{
		{
			name:  "plain json",
			input: `{"name":"Pancakes","servings":2,"ingredients":["flour","milk"],"steps":["mix","cook"]}`,
			want:  Recipe{Name: "Pancakes", Servings: 2, Ingredients: []string{"flour", "milk"}, Steps: []string{"mix", "cook"}},
		},
		{
			name:  "fenced json",
			input: "```json\n{\"name\":\"Toast\",\"servings\":1,\"ingredients\":[\"bread\"],\"steps\":[\"toast\"]}\n```",
			want:  Recipe{Name: "Toast", Servings: 1, Ingredients: []string{"bread"}, Steps: []string{"toast"}},
		},
		{
			name:    "invalid json",
			input:   "not json at all",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRecipe(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRecipe: %v", err)
			}
			if got.Name != tt.want.Name || got.Servings != tt.want.Servings {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
			if len(got.Ingredients) != len(tt.want.Ingredients) || len(got.Steps) != len(tt.want.Steps) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
