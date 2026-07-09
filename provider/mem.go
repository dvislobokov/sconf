package provider

// MapProvider отдаёт заранее заданные значения. Ключи могут использовать
// разделитель ":" (например "database:host").
type MapProvider map[string]string

// Map создаёт источник из готовой карты значений.
func Map(values map[string]string) MapProvider {
	m := make(MapProvider, len(values))
	for k, v := range values {
		m[k] = v
	}
	return m
}

func (p MapProvider) Load() (map[string]string, error) {
	return p, nil
}
