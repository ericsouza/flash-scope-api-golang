package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
)

type Flash struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

var (
	ctx = context.Background()
)

func extractSubFromToken(c echo.Context) (string, error) {
	// Obter o cabeçalho de autorização
	authHeader := c.Request().Header.Get("Authorization")

	if authHeader == "" {
		return "", fmt.Errorf("authorization header is missing")
	}

	// Extrair o token da string "Bearer <token>"
	tokenString := authHeader[len("Bearer "):]

	// Parse do token sem verificar a assinatura
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return "", fmt.Errorf("error parsing token: %v", err)
	}

	// Obter o campo "sub"
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		sub, exists := claims["sub"]
		if !exists {
			return "", fmt.Errorf("sub not found in token")
		}
		// Converter o campo "sub" para string e retornar
		return sub.(string), nil
	}

	return "", fmt.Errorf("invalid token claims")
}

// Handler para o endpoint de liveness com verificação de conexão Redis
func readinessHandler(c echo.Context) error {
	rdb := c.Get(string(redisClientKey)).(*redis.Client)

	// Verifica a conexão com Redis
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "DOWN",
			"message": "Redis connection failed",
		})
	}

	// Se a conexão estiver ok, retorna um status 200 OK
	return c.JSON(http.StatusOK, echo.Map{
		"status": "UP",
	})
}

// Handler para o endpoint de readiness
func livenessHandler(c echo.Context) error {
	// O endpoint de readiness pode fazer verificações adicionais, como verificar se o banco de dados está disponível
	// Por simplicidade, estamos apenas retornando OK
	return c.JSON(http.StatusServiceUnavailable, echo.Map{
		"status": "UP",
	})
}

func getMessages(c echo.Context) error {
	sub := c.Get(string(currentUserKey)).(string)
	// Recupera o cliente Redis do contexto
	rdb := c.Get("redisClient").(*redis.Client)

	// Nome da chave Redis
	redisKey := "message::" + sub

	flashes := []Flash{}

	// Transação Redis para garantir que LRANGE e DEL aconteçam juntos
	err := rdb.Watch(ctx, func(tx *redis.Tx) error {
		// Ler todas as mensagens da lista com LRANGE
		rawMessages, err := tx.LRange(ctx, redisKey, 0, -1).Result()
		if err != nil {
			return err
		}

		// Deletar a chave da lista com DEL
		if err := tx.Del(ctx, redisKey).Err(); err != nil {
			return err
		}

		// Deserializar cada mensagem para a estrutura Message
		for _, rawMessage := range rawMessages {
			var msg Flash
			if err := json.Unmarshal([]byte(rawMessage), &msg); err != nil {
				return err
			}
			flashes = append(flashes, msg)
		}

		return nil
	}, redisKey)

	if err != nil {
		log.Printf("Failed to read and delete messages: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"error": "Failed to read and delete messages from Redis",
		})
	}

	// Retornar as mensagens como JSON
	return c.JSON(http.StatusOK, flashes)
}

func addMessages(c echo.Context) error {
	var flashes []Flash
	// Fazer o Bind do JSON recebido na estrutura Message
	if err := c.Bind(&flashes); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error": "Invalid request",
		})
	}
	sub := c.Get(string(currentUserKey)).(string)
	redisKey := "message::" + sub
	rdb := c.Get(string(redisClientKey)).(*redis.Client)

	// Serializar todas as mensagens para armazenar no Redis
	var serializedMessages []interface{}
	for _, flash := range flashes {
		serializedMessage, err := json.Marshal(flash)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, echo.Map{
				"error": "Failed to serialize messages",
			})
		}
		serializedMessages = append(serializedMessages, string(serializedMessage))
	}

	// Usar pipeline para executar RPUSH e EXPIRE
	_, err := rdb.Pipeline().Pipelined(ctx, func(pipeliner redis.Pipeliner) error {
		// Adicionar a lista de mensagens na lista Redis com RPUSH
		pipeliner.RPush(ctx, redisKey, serializedMessages...)

		// Atualizar o TTL da chave para 60 segundos
		pipeliner.Expire(ctx, redisKey, 60*time.Second)

		return nil
	})

	if err != nil {
		log.Printf("Failed to save messages: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"error": "Failed to save messages to Redis",
		})
	}

	return c.NoContent(http.StatusNoContent)
}

type contextKey string

const redisClientKey contextKey = "redisClient"
const currentUserKey contextKey = "currentUser"

func loadCommomMiddlewares(e *echo.Echo, rdb *redis.Client) {
	e.Use(echoprometheus.NewMiddleware("myapp")) // adds middleware to gather metrics
	e.GET("/q/metrics", echoprometheus.NewHandler())
	e.GET("q/health/live", livenessHandler)
	e.GET("q/health/ready", readinessHandler)

	// Middleware para injetar o cliente Redis no contexto de cada requisição
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Adicionar o cliente Redis ao contexto
			c.Set(string(redisClientKey), rdb)
			return next(c)
		}
	})
}

func loadAuthenticationMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		sub, err := extractSubFromToken(c)
		if err != nil {
			return c.NoContent(http.StatusUnauthorized)
		}
		c.Set(string(currentUserKey), sub)
		return next(c)
	}

}

func main() {
	// Criar conexão com Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379", // endereço do Redis
		DB:   0,                // use o banco de dados padrão
	})

	// Criar uma nova instância do Echo
	e := echo.New()
	// Carregar os middlewares
	loadCommomMiddlewares(e, rdb)

	userGroup := e.Group("/api/v1/user")

	userGroup.Use(loadAuthenticationMiddleware)

	userGroup.GET("/api/v1/user/flash", getMessages)
	userGroup.POST("/api/v1/user/flash", addMessages)

	// Iniciar o servidor na porta 5770
	e.Logger.Fatal(e.Start(":5770"))
}
