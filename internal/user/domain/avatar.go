package domain

import "strings"

// PublicAvatarURL возвращает ссылку для публичного CDN или пустую строку при отсутствии ключа.
func PublicAvatarURL(baseURL, key string) string {
	if key == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/" + key
}
