package settings

type NotificationChannel string

const (
	NotificationChannelDingtalk NotificationChannel = "dingtalk"
	NotificationChannelFeishu   NotificationChannel = "feishu"
	NotificationChannelTelegram NotificationChannel = "telegram"
)

type TestNotificationRequest struct {
	Channel          NotificationChannel `json:"channel"`
	Webhook          string              `json:"webhook"`
	Secret           string              `json:"secret"`
	TelegramBotToken string              `json:"telegramBotToken"`
	TelegramChatID   string              `json:"telegramChatId"`
	TelegramProxyURL string              `json:"telegramProxyUrl"`
}

type TestNotificationResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type NotificationChannelSettings struct {
	Dingtalk []DingtalkChannelSettings `json:"dingtalk"`
	Feishu   []WebhookChannelSettings  `json:"feishu"`
	Telegram []TelegramChannelSettings `json:"telegram"`
}

type StrategySettings struct {
	EnableRefreshInterval      bool     `json:"enableRefreshInterval"`
	RefreshInterval            int      `json:"refreshInterval"`
	EnableBalanceWarning       bool     `json:"enableBalanceWarning"`
	DefaultBalanceThreshold    float64  `json:"defaultBalanceThreshold"`
	BalanceNotifyBotIDs        []string `json:"balanceNotifyBotIds"`
	BalanceTemplate            string   `json:"balanceTemplate"`
	EnableMultiplierAlert      bool     `json:"enableMultiplierAlert"`
	MultiplierNotifyBotIDs     []string `json:"multiplierNotifyBotIds"`
	MultiplierTemplate         string   `json:"multiplierTemplate"`
	EnableAutoChangeMultiplier bool     `json:"enableAutoChangeMultiplier"`
}

type DingtalkChannelSettings struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Webhook string `json:"webhook"`
	Secret  string `json:"secret"`
}

type WebhookChannelSettings struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Webhook string `json:"webhook"`
	Secret  string `json:"secret"`
}

type TelegramChannelSettings struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"botToken"`
	ChatID   string `json:"chatId"`
	ProxyURL string `json:"proxyUrl"`
}
