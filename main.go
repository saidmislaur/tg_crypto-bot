package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

const defaultConfigPath = "rate_config.json"

var rateTiers = []string{
	"до 1000 USDT",
	"1000–5000 USDT",
	"5000–10000 USDT",
	"10000 USDT и выше",
}

type Rates map[string]struct {
	Sell string `json:"sell"`
	Buy  string `json:"buy"`
}

type RapiraRatesResponse struct {
	Data    []RapiraRate `json:"data"`
	Code    int          `json:"code"`
	Message string       `json:"message"`
}

type RapiraRate struct {
	Symbol   string  `json:"symbol"`
	AskPrice float64 `json:"askPrice"`
	BidPrice float64 `json:"bidPrice"`
}

type RateConfig struct {
	Title           string    `json:"title"`
	Locations       []string  `json:"locations"`
	ManualBid       float64   `json:"manual_bid"`
	ManualAsk       float64   `json:"manual_ask"`
	ForceManualRate bool      `json:"force_manual_rate"`
	BuyAdjustments  []float64 `json:"buy_adjustments"`
	SellAdjustments []float64 `json:"sell_adjustments"`
	Signature       string    `json:"signature"`
	AboutText       string    `json:"about_text"`
}

type AdminState struct {
	Field string
}

var (
	configPath    string
	configMu      sync.RWMutex
	currentConfig RateConfig
	adminStates   = map[int64]AdminState{}
)

func defaultConfig() RateConfig {
	return RateConfig{
		Title: "КУРС USDT ↔️ RUB",
		Locations: []string{
			"📍Назрань ул. Московская 4а",
			"📍Карабулак ул. Джабагиева 2а",
		},
		BuyAdjustments:  []float64{-0.47, -0.37, -0.27, -0.17},
		SellAdjustments: []float64{0.53, 0.43, 0.33, 0.23},
		ManualBid:       75.48,
		ManualAsk:       76.08,
		ForceManualRate: false,
		Signature:       "обменник Cryptoclub ☎️ +7 (918) 813-28-15",
		AboutText: `Мы Cryptoclub_zr
Ваш надежный партнер в мире криптовалюты

• Покупка продажа usdt
• Вывести деньги с биржи без риска
• Отправить деньги за границу или принять из-за рубежа

✅Все сделки строго по законам шариата

Уникальная услуга в РФ🇷🇺
• В наших офисах P2P вы можете подписать контракт на 4 месяца
🔥с бесплатным обучением и работой в нашем офисе

🔻Так же продажа и обслуживание майнеров

Наш адрес
г. Назрань Московская 4а
г. Карабулак Джабагиева 2а

📌instagram @cryptoclub_zr

☎️ +7918 813-28-15
☎️+7988-8015-21-37`,
	}
}

func normalizeConfig(cfg *RateConfig) {
	defaults := defaultConfig()
	if strings.TrimSpace(cfg.Title) == "" {
		cfg.Title = defaults.Title
	}
	if len(cfg.Locations) == 0 {
		cfg.Locations = defaults.Locations
	}
	if len(cfg.BuyAdjustments) != 4 {
		cfg.BuyAdjustments = defaults.BuyAdjustments
	}
	if len(cfg.SellAdjustments) != 4 {
		cfg.SellAdjustments = defaults.SellAdjustments
	}
	if cfg.ManualBid <= 0 {
		cfg.ManualBid = defaults.ManualBid
	}
	if cfg.ManualAsk <= 0 {
		cfg.ManualAsk = defaults.ManualAsk
	}
	if strings.TrimSpace(cfg.Signature) == "" {
		cfg.Signature = defaults.Signature
	}
	if strings.TrimSpace(cfg.AboutText) == "" {
		cfg.AboutText = defaults.AboutText
	}
}

func loadConfig(path string) (RateConfig, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, saveConfig(path, cfg)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	normalizeConfig(&cfg)
	return cfg, nil
}

func saveConfig(path string, cfg RateConfig) error {
	normalizeConfig(&cfg)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func getConfig() RateConfig {
	configMu.RLock()
	defer configMu.RUnlock()

	cfg := currentConfig
	cfg.Locations = append([]string(nil), currentConfig.Locations...)
	cfg.BuyAdjustments = append([]float64(nil), currentConfig.BuyAdjustments...)
	cfg.SellAdjustments = append([]float64(nil), currentConfig.SellAdjustments...)
	return cfg
}

func updateConfig(cfg RateConfig) error {
	normalizeConfig(&cfg)
	if err := saveConfig(configPath, cfg); err != nil {
		return err
	}

	configMu.Lock()
	currentConfig = cfg
	configMu.Unlock()
	return nil
}

func fetchJSON(client *http.Client, url string, target any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s вернул статус %s", url, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("не удалось разобрать ответ %s: %w", url, err)
	}

	return nil
}

func fetchRates(client *http.Client) (Rates, error) {
	var rates Rates
	if err := fetchJSON(client, "https://grinex.io/rates?offset=0", &rates); err != nil {
		return nil, err
	}
	return rates, nil
}

func parseNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case string:
		number, err := strconv.ParseFloat(v, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func parseRatePair(data map[string]any) (bid, ask float64, ok bool) {
	if ticker, exists := data["ticker"].(map[string]any); exists {
		data = ticker
	}

	bid, bidOK := parseNumber(data["buy"])
	if !bidOK {
		bid, bidOK = parseNumber(data["bid"])
	}
	ask, askOK := parseNumber(data["sell"])
	if !askOK {
		ask, askOK = parseNumber(data["ask"])
	}

	return bid, ask, bidOK && askOK && bid > 0 && ask > 0
}

func fetchLegacyRate(client *http.Client) (bid, ask float64, err error) {
	rates, err := fetchRates(client)
	if err != nil {
		return 0, 0, err
	}

	for _, market := range []string{"usdtrub", "usdta7a5"} {
		rate, ok := rates[market]
		if !ok || rate.Buy == "" || rate.Sell == "" {
			continue
		}

		bid, err = strconv.ParseFloat(rate.Buy, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("не удалось прочитать курс покупки %s: %w", market, err)
		}
		ask, err = strconv.ParseFloat(rate.Sell, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("не удалось прочитать курс продажи %s: %w", market, err)
		}
		return bid, ask, nil
	}

	return 0, 0, fmt.Errorf("курс USDT/RUB недоступен в старом ответе Grinex")
}

func fetchTickerRate(client *http.Client, url string) (bid, ask float64, err error) {
	var data map[string]any
	if err := fetchJSON(client, url, &data); err != nil {
		return 0, 0, err
	}

	if bid, ask, ok := parseRatePair(data); ok {
		return bid, ask, nil
	}

	for _, market := range []string{"usdtrub", "usdta7a5"} {
		marketData, ok := data[market].(map[string]any)
		if !ok {
			continue
		}
		if bid, ask, ok := parseRatePair(marketData); ok {
			return bid, ask, nil
		}
	}

	return 0, 0, fmt.Errorf("в ответе %s нет bid/ask для USDT/RUB", url)
}

func priceFromOrderBookSide(side any) (float64, bool) {
	levels, ok := side.([]any)
	if !ok || len(levels) == 0 {
		return 0, false
	}

	firstLevel, ok := levels[0].([]any)
	if ok && len(firstLevel) > 0 {
		return parseNumber(firstLevel[0])
	}

	if level, ok := levels[0].(map[string]any); ok {
		if price, ok := parseNumber(level["price"]); ok {
			return price, true
		}
	}

	return 0, false
}

func fetchOrderBookRate(client *http.Client, url string) (bid, ask float64, err error) {
	var data map[string]any
	if err := fetchJSON(client, url, &data); err != nil {
		return 0, 0, err
	}

	bid, bidOK := priceFromOrderBookSide(data["bids"])
	ask, askOK := priceFromOrderBookSide(data["asks"])
	if !bidOK || !askOK || bid <= 0 || ask <= 0 {
		return 0, 0, fmt.Errorf("в стакане %s нет bid/ask", url)
	}

	return bid, ask, nil
}

func fetchExternalRate() (bid, ask float64, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	var response RapiraRatesResponse
	if err := fetchJSON(client, "https://api.rapira.net/open/market/rates", &response); err != nil {
		return 0, 0, err
	}
	if response.Code != 0 {
		return 0, 0, fmt.Errorf("Rapira вернула код %d: %s", response.Code, response.Message)
	}

	for _, rate := range response.Data {
		if rate.Symbol != "USDT/RUB" {
			continue
		}
		if rate.BidPrice <= 0 || rate.AskPrice <= 0 {
			return 0, 0, fmt.Errorf("Rapira вернула некорректный USDT/RUB: bid %.2f, ask %.2f", rate.BidPrice, rate.AskPrice)
		}
		return rate.BidPrice, rate.AskPrice, nil
	}

	return 0, 0, errors.New("Rapira не вернула пару USDT/RUB")
}

func getCurrentRate() (bid, ask float64, err error) {
	cfg := getConfig()
	if cfg.ForceManualRate && cfg.ManualBid > 0 && cfg.ManualAsk > 0 {
		return cfg.ManualBid, cfg.ManualAsk, nil
	}

	bid, ask, err = fetchExternalRate()
	if err == nil {
		return bid, ask, nil
	}

	if cfg.ManualBid > 0 && cfg.ManualAsk > 0 {
		log.Printf("не удалось получить курс автоматически, используем ручной курс: %v", err)
		return cfg.ManualBid, cfg.ManualAsk, nil
	}

	return 0, 0, fmt.Errorf("автоматический курс недоступен, ручной курс не задан: %w", err)
}

func generateRateText() string {
	bid, ask, err := getCurrentRate()
	if err != nil {
		return fmt.Sprintf("Ошибка получения курса: %v", err)
	}

	cfg := getConfig()
	buy1 := bid + cfg.BuyAdjustments[0]
	buy2 := bid + cfg.BuyAdjustments[1]
	buy3 := bid + cfg.BuyAdjustments[2]
	buy4 := bid + cfg.BuyAdjustments[3]

	sell1 := ask + cfg.SellAdjustments[0]
	sell2 := ask + cfg.SellAdjustments[1]
	sell3 := ask + cfg.SellAdjustments[2]
	sell4 := ask + cfg.SellAdjustments[3]

	return fmt.Sprintf(
		"%s\n"+
			"%s\n\n"+
			"МЫ ПОКУПАЕМ USDT У ВАС:\n"+
			"• до 1000: %.2f RUB\n"+
			"• 1000–5000: %.2f RUB\n"+
			"• 5000–10000: %.2f RUB\n"+
			"• от 10000 и выше: %.2f RUB\n\n"+
			"☛ МЫ ПРОДАЕМ USDT ВАМ:\n"+
			"• до 1000: %.2f RUB\n"+
			"• 1000–5000: %.2f RUB\n"+
			"• 5000–10000: %.2f RUB\n"+
			"• от 10000 и выше: %.2f RUB\n\n"+
			"%s",
		cfg.Title,
		strings.Join(cfg.Locations, "\n"),
		buy1, buy2, buy3, buy4,
		sell1, sell2, sell3, sell4,
		cfg.Signature,
	)
}

func parseAdminIDs(raw string) map[int64]bool {
	admins := make(map[int64]bool)
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	}) {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			log.Printf("ADMIN_IDS: пропускаем некорректный ID %q", part)
			continue
		}
		admins[id] = true
	}
	return admins
}

func isAdmin(user *tgbotapi.User, admins map[int64]bool) bool {
	return user != nil && admins[user.ID]
}

func mainMenu(showAdmin bool) tgbotapi.ReplyKeyboardMarkup {
	rows := [][]tgbotapi.KeyboardButton{
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("Актуальный курс")),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("О нас")),
	}
	if showAdmin {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("Админ-панель")))
	}

	menu := tgbotapi.NewReplyKeyboard(rows...)
	menu.ResizeKeyboard = true
	return menu
}

func adminPanelMarkup() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Курс", "admin:course"),
			tgbotapi.NewInlineKeyboardButtonData("Тексты", "admin:text"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Предпросмотр", "admin:preview"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Обновить", "admin:home"),
		),
	)
}

func coursePanelMarkup() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Базовый курс", "admin:edit:manual_rate"),
			tgbotapi.NewInlineKeyboardButtonData("Ручной/авто", "admin:toggle_manual"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Покупка до 1000", "admin:edit:buy_0"),
			tgbotapi.NewInlineKeyboardButtonData("Продажа до 1000", "admin:edit:sell_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Покупка 1000–5000", "admin:edit:buy_1"),
			tgbotapi.NewInlineKeyboardButtonData("Продажа 1000–5000", "admin:edit:sell_1"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Покупка 5000–10000", "admin:edit:buy_2"),
			tgbotapi.NewInlineKeyboardButtonData("Продажа 5000–10000", "admin:edit:sell_2"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Покупка 10000+", "admin:edit:buy_3"),
			tgbotapi.NewInlineKeyboardButtonData("Продажа 10000+", "admin:edit:sell_3"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Все покупки", "admin:edit:buy"),
			tgbotapi.NewInlineKeyboardButtonData("Все продажи", "admin:edit:sell"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Предпросмотр", "admin:preview"),
			tgbotapi.NewInlineKeyboardButtonData("Назад", "admin:home"),
		),
	)
}

func textPanelMarkup() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Заголовок", "admin:edit:title"),
			tgbotapi.NewInlineKeyboardButtonData("Адреса", "admin:edit:locations"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Подпись", "admin:edit:signature"),
			tgbotapi.NewInlineKeyboardButtonData("О нас", "admin:edit:about"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Предпросмотр", "admin:preview"),
			tgbotapi.NewInlineKeyboardButtonData("Назад", "admin:home"),
		),
	)
}

func formatSigned(value float64) string {
	if value >= 0 {
		return fmt.Sprintf("+%.2f", value)
	}
	return fmt.Sprintf("%.2f", value)
}

func formatAdjustments(base string, values []float64) string {
	var lines []string
	for i, tier := range rateTiers {
		lines = append(lines, fmt.Sprintf("• %s: %s %s", tier, base, formatSigned(values[i])))
	}
	return strings.Join(lines, "\n")
}

func formatVisibleRates(base float64, adjustments []float64) string {
	var lines []string
	for i, tier := range rateTiers {
		lines = append(lines, fmt.Sprintf("• %s: %.2f RUB  (%s)", tier, base+adjustments[i], formatSigned(adjustments[i])))
	}
	return strings.Join(lines, "\n")
}

func adminPanelText() string {
	cfg := getConfig()
	return fmt.Sprintf(
		"Админ-панель\n\n"+
			"Что хотите изменить?\n\n"+
			"Курс: %s\n"+
			"База: покупка %.2f / продажа %.2f\n\n"+
			"Кнопка «Курс» меняет цифры.\n"+
			"Кнопка «Тексты» меняет адреса и подпись.\n"+
			"Кнопка «Предпросмотр» показывает сообщение как для клиента.",
		rateModeText(cfg.ForceManualRate),
		cfg.ManualBid,
		cfg.ManualAsk,
	)
}

func coursePanelText() string {
	cfg := getConfig()
	return fmt.Sprintf(
		"Настройка курса\n\n"+
			"Режим: %s\n"+
			"Базовый курс: покупка %.2f / продажа %.2f\n\n"+
			"Что видит клиент:\n\n"+
			"МЫ ПОКУПАЕМ USDT У ВАС:\n%s\n\n"+
			"МЫ ПРОДАЕМ USDT ВАМ:\n%s\n\n"+
			"Число в скобках — это прибавка к базовому курсу. Нажмите нужную строку и отправьте одно число, например +0.95.",
		rateModeText(cfg.ForceManualRate),
		cfg.ManualBid,
		cfg.ManualAsk,
		formatVisibleRates(cfg.ManualBid, cfg.BuyAdjustments),
		formatVisibleRates(cfg.ManualAsk, cfg.SellAdjustments),
	)
}

func textPanelText() string {
	cfg := getConfig()
	return fmt.Sprintf(
		"Тексты и контакты\n\n"+
			"Заголовок:\n%s\n\n"+
			"Адреса:\n%s\n\n"+
			"Подпись:\n%s\n\n"+
			"Выберите, что нужно изменить.",
		cfg.Title,
		strings.Join(cfg.Locations, "\n"),
		cfg.Signature,
	)
}

func rateModeText(forceManual bool) string {
	if forceManual {
		return "ручной"
	}
	return "авто с ручным резервом"
}

func sendAdminPanel(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, adminPanelText())
	msg.ReplyMarkup = adminPanelMarkup()
	if _, err := bot.Send(msg); err != nil {
		log.Printf("не удалось отправить админ-панель: %v", err)
	}
}

func sendCoursePanel(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, coursePanelText())
	msg.ReplyMarkup = coursePanelMarkup()
	if _, err := bot.Send(msg); err != nil {
		log.Printf("не удалось отправить настройки курса: %v", err)
	}
}

func sendTextPanel(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, textPanelText())
	msg.ReplyMarkup = textPanelMarkup()
	if _, err := bot.Send(msg); err != nil {
		log.Printf("не удалось отправить настройки текстов: %v", err)
	}
}

func sendRatePreview(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Предпросмотр сообщения для клиента:\n\n"+generateRateText())
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Курс", "admin:course"),
			tgbotapi.NewInlineKeyboardButtonData("Назад", "admin:home"),
		),
	)
	if _, err := bot.Send(msg); err != nil {
		log.Printf("не удалось отправить предпросмотр: %v", err)
	}
}

func handleAdminCallback(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, admins map[int64]bool) {
	callback := tgbotapi.NewCallback(query.ID, "")
	if _, err := bot.Request(callback); err != nil {
		log.Printf("не удалось ответить на callback: %v", err)
	}

	if !isAdmin(query.From, admins) {
		denied := tgbotapi.NewMessage(query.Message.Chat.ID, "Нет доступа к админ-панели.")
		bot.Send(denied)
		return
	}

	switch query.Data {
	case "admin:home", "admin:refresh":
		sendAdminPanel(bot, query.Message.Chat.ID)
		return
	case "admin:course":
		sendCoursePanel(bot, query.Message.Chat.ID)
		return
	case "admin:text":
		sendTextPanel(bot, query.Message.Chat.ID)
		return
	case "admin:preview":
		sendRatePreview(bot, query.Message.Chat.ID)
		return
	case "admin:toggle_manual":
		cfg := getConfig()
		cfg.ForceManualRate = !cfg.ForceManualRate
		if err := updateConfig(cfg); err != nil {
			msg := tgbotapi.NewMessage(query.Message.Chat.ID, fmt.Sprintf("Не удалось изменить режим курса: %v", err))
			bot.Send(msg)
			return
		}
		sendCoursePanel(bot, query.Message.Chat.ID)
		return
	}

	if !strings.HasPrefix(query.Data, "admin:edit:") {
		return
	}

	field := strings.TrimPrefix(query.Data, "admin:edit:")
	adminStates[query.From.ID] = AdminState{Field: field}

	prompt := adminPrompt(field)
	msg := tgbotapi.NewMessage(query.Message.Chat.ID, prompt)
	bot.Send(msg)
}

func adminPrompt(field string) string {
	if side, _, tier, ok := adjustmentFieldInfo(field); ok {
		base := "bid"
		action := "покупки"
		if side == "sell" {
			base = "ask"
			action = "продажи"
		}
		return fmt.Sprintf(
			"Отправьте поправку для %s: %s.\nНапример: +0.95 или -0.47\n\nИтог считается так: %s + поправка.",
			action,
			tier,
			base,
		)
	}

	switch field {
	case "title":
		return "Отправьте новый заголовок одним сообщением.\nНапример: КУРС USDT ↔️ RUB"
	case "locations":
		return "Отправьте адреса одним сообщением. Каждый адрес пишите с новой строки.\n\nНапример:\n📍Назрань ул. Московская 4а\n📍Карабулак ул. Джабагиева 2а"
	case "buy":
		return "Отправьте 4 поправки для покупки через пробел.\nПорядок: до 1000, 1000–5000, 5000–10000, 10000+.\n\nНапример: -0.47 -0.37 -0.27 -0.17"
	case "sell":
		return "Отправьте 4 поправки для продажи через пробел.\nПорядок: до 1000, 1000–5000, 5000–10000, 10000+.\n\nНапример: +0.53 +0.43 +0.33 +0.23"
	case "manual_rate":
		return "Отправьте базовый курс покупки и продажи через пробел.\nПервое число — покупка, второе — продажа.\n\nНапример: 75.48 76.08"
	case "signature":
		return "Отправьте новую подпись в конце сообщения курса.\nНапример: обменник Cryptoclub ☎️ +7 (918) 813-28-15"
	case "about":
		return "Отправьте новый текст раздела «О нас» одним сообщением."
	default:
		return "Отправьте новое значение."
	}
}

func adjustmentFieldInfo(field string) (side string, index int, tier string, ok bool) {
	parts := strings.Split(field, "_")
	if len(parts) != 2 || (parts[0] != "buy" && parts[0] != "sell") {
		return "", 0, "", false
	}

	index, err := strconv.Atoi(parts[1])
	if err != nil || index < 0 || index >= len(rateTiers) {
		return "", 0, "", false
	}

	return parts[0], index, rateTiers[index], true
}

func parseOneFloat(text string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(text), ",", "."), "+"), 64)
	if err != nil {
		return 0, fmt.Errorf("число %q не удалось прочитать", text)
	}
	return value, nil
}

func parseFourFloats(text string) ([]float64, error) {
	parts := strings.Fields(strings.ReplaceAll(text, ",", "."))
	if len(parts) != 4 {
		return nil, fmt.Errorf("нужно ровно 4 числа, получено %d", len(parts))
	}

	values := make([]float64, 4)
	for i, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimPrefix(part, "+"), 64)
		if err != nil {
			return nil, fmt.Errorf("число %q не удалось прочитать", part)
		}
		values[i] = value
	}
	return values, nil
}

func parseTwoFloats(text string) (float64, float64, error) {
	parts := strings.Fields(strings.ReplaceAll(text, ",", "."))
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("нужно ровно 2 числа: bid и ask")
	}

	bid, err := strconv.ParseFloat(strings.TrimPrefix(parts[0], "+"), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bid %q не удалось прочитать", parts[0])
	}
	ask, err := strconv.ParseFloat(strings.TrimPrefix(parts[1], "+"), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("ask %q не удалось прочитать", parts[1])
	}
	if bid <= 0 || ask <= 0 {
		return 0, 0, fmt.Errorf("bid и ask должны быть больше нуля")
	}

	return bid, ask, nil
}

func nonEmptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func isCourseField(field string) bool {
	if field == "buy" || field == "sell" || field == "manual_rate" {
		return true
	}
	_, _, _, ok := adjustmentFieldInfo(field)
	return ok
}

func isTextField(field string) bool {
	return field == "title" || field == "locations" || field == "signature" || field == "about"
}

func handleAdminInput(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	state, ok := adminStates[message.From.ID]
	if !ok {
		return
	}

	text := strings.TrimSpace(message.Text)
	if text == "" {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Пустое значение не сохранено. Отправьте значение или /cancel.")
		bot.Send(msg)
		return
	}

	cfg := getConfig()
	var err error

	if side, index, _, ok := adjustmentFieldInfo(state.Field); ok {
		value, parseErr := parseOneFloat(text)
		if parseErr != nil {
			err = parseErr
		} else if side == "buy" {
			cfg.BuyAdjustments[index] = value
		} else {
			cfg.SellAdjustments[index] = value
		}
	} else {
		switch state.Field {
		case "title":
			cfg.Title = text
		case "locations":
			cfg.Locations = nonEmptyLines(text)
		case "buy":
			cfg.BuyAdjustments, err = parseFourFloats(text)
		case "sell":
			cfg.SellAdjustments, err = parseFourFloats(text)
		case "manual_rate":
			cfg.ManualBid, cfg.ManualAsk, err = parseTwoFloats(text)
		case "signature":
			cfg.Signature = text
		case "about":
			cfg.AboutText = message.Text
		default:
			err = fmt.Errorf("неизвестное поле настройки")
		}
	}

	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Не удалось сохранить: %v", err))
		bot.Send(msg)
		return
	}

	if err := updateConfig(cfg); err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Ошибка записи настроек: %v", err))
		bot.Send(msg)
		return
	}

	delete(adminStates, message.From.ID)

	msg := tgbotapi.NewMessage(message.Chat.ID, "Сохранено. Ниже актуальные настройки.")
	bot.Send(msg)
	if isCourseField(state.Field) {
		sendCoursePanel(bot, message.Chat.ID)
		return
	}
	if isTextField(state.Field) {
		sendTextPanel(bot, message.Chat.ID)
		return
	}
	sendAdminPanel(bot, message.Chat.ID)
}

func startHealthServer() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("crypto bot is running"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	go func() {
		addr := ":" + port
		log.Printf("Health server listening on %s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("Health server stopped: %v", err)
		}
	}()
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Не удалось загрузить .env, используем системные переменные")
	}

	startHealthServer()

	configPath = os.Getenv("RATE_CONFIG_PATH")
	if configPath == "" {
		configPath = defaultConfigPath
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Не удалось загрузить настройки: %v", err)
	}
	currentConfig = cfg

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("BOT_TOKEN не задан в .env")
	}

	admins := parseAdminIDs(os.Getenv("ADMIN_IDS"))
	if len(admins) == 0 {
		log.Println("ADMIN_IDS не задан: админ-панель будет недоступна. Узнать ID можно командой /id.")
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatal(err)
	}

	bot.Debug = false

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.CallbackQuery != nil {
			handleAdminCallback(bot, update.CallbackQuery, admins)
			continue
		}

		if update.Message == nil {
			continue
		}

		admin := isAdmin(update.Message.From, admins)
		menu := mainMenu(admin)

		if update.Message.Text == "/cancel" {
			delete(adminStates, update.Message.From.ID)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Отменено.")
			msg.ReplyMarkup = menu
			bot.Send(msg)
			continue
		}

		if admin {
			if _, waiting := adminStates[update.Message.From.ID]; waiting {
				handleAdminInput(bot, update.Message)
				continue
			}
		}

		switch update.Message.Text {
		case "/start":
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Добро пожаловать! Выберите действие:")
			msg.ReplyMarkup = menu
			bot.Send(msg)
		case "/id":
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Ваш Telegram ID: %d", update.Message.From.ID))
			msg.ReplyMarkup = menu
			bot.Send(msg)
		case "/admin", "Админ-панель":
			if !admin {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Нет доступа. Ваш Telegram ID: %d", update.Message.From.ID))
				msg.ReplyMarkup = menu
				bot.Send(msg)
				continue
			}
			sendAdminPanel(bot, update.Message.Chat.ID)
		case "Актуальный курс":
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, generateRateText())
			msg.ReplyMarkup = menu
			bot.Send(msg)
		case "О нас":
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, getConfig().AboutText)
			msg.ReplyMarkup = menu
			bot.Send(msg)
		}
	}
}
