package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

type Rates map[string]struct {
	Sell string `json:"sell"`
	Buy  string `json:"buy"`
}

func fetchRates() (Rates, error) {
	url := "https://grinex.io/rates?offset=0"
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rates Rates
	if err := json.Unmarshal(body, &rates); err != nil {
		return nil, err
	}

	return rates, nil
}

func getCurrentRate() (bid, ask float64, err error) {
	rates, err := fetchRates()
	if err != nil {
		return 0, 0, err
	}

	usdtrub, ok := rates["usdta7a5"]
	if !ok || usdtrub.Buy == "" || usdtrub.Sell == "" {
		return 0, 0, fmt.Errorf("курс USDT/RUB недоступен")
	}

	_, err = fmt.Sscanf(usdtrub.Buy, "%f", &bid)
	if err != nil {
		return 0, 0, err
	}
	_, err = fmt.Sscanf(usdtrub.Sell, "%f", &ask)
	if err != nil {
		return 0, 0, err
	}

	return bid, ask, nil
}

func generateRateText() string {
	bid, ask, err := getCurrentRate()
	if err != nil {
		return fmt.Sprintf("Ошибка получения курса: %v", err)
	}

	buy1 := bid - 0.65
	buy2 := bid - 0.55
	buy3 := bid - 0.45
	buy4 := bid - 0.35

	sell1 := ask + 0.93
	sell2 := ask + 0.83
	sell3 := ask + 0.67
	sell4 := ask + 0.57

	return fmt.Sprintf(
		"КУРС USDT ↔️ RUB\n☛"+
			"📍Назрань ул. Московская 4а"+
			"📍Карабулак ул. Осканова 5а\n\n"+
			"МЫ ПОКУПАЕМ USDT У ВАС:\n"+
			"• до 1000 USDT: %.2f RUB\n"+
			"• 1000–5000 USDT: %.2f RUB\n"+
			"• 5000–10000 USDT: %.2f RUB\n"+
			"• 10000 USDT и выше: %.2f RUB\n\n"+
			"☛ МЫ ПРОДАЕМ USDT ВАМ:\n"+
			"• до 1000 USDT: %.2f RUB\n"+
			"• 1000–5000 USDT: %.2f RUB\n"+
			"• 5000–10000 USDT: %.2f RUB\n"+
			"• 10000 USDT и выше: %.2f RUB\n\ns"+
			"обменник Cryptoclub ☎️ +7 (918) 813-28-15",
		buy1, buy2, buy3, buy4,
		sell1, sell2, sell3, sell4,
	)
}

func main() {
	// Загружаем переменные окружения из .env
	err := godotenv.Load()
	if err != nil {
		log.Println("Не удалось загрузить .env, используем системные переменные")
	}

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("BOT_TOKEN не задан в .env")
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
		if update.Message == nil {
			continue
		}

		menu := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("Актуальный курс"),
			),
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("О нас"),
			),
		)

		switch update.Message.Text {
		case "/start":
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Добро пожаловать! Выберите действие:")
			msg.ReplyMarkup = menu
			bot.Send(msg)
		case "Актуальный курс":
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, generateRateText())
			bot.Send(msg)
		case "О нас":
			msg := tgbotapi.NewMessage(update.Message.Chat.ID,
				`Мы Cryptoclub_zr 
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
				г. Карабулак Осканова 5а 

				📌instagram @cryptoclub_zr 

				☎️ +7918 813-28-15
				☎️+7988-8015-21-37`)
			bot.Send(msg)
		}
	}
}
