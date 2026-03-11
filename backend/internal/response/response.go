package response

import "github.com/gofiber/fiber/v2"

type Envelope struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func JSON(c *fiber.Ctx, status int, message string, data interface{}) error {
	code := status
	if status >= 200 && status < 300 {
		code = 0
	}
	return c.Status(status).JSON(Envelope{Code: code, Message: message, Data: data})
}

func OK(c *fiber.Ctx, data interface{}) error {
	return JSON(c, fiber.StatusOK, "ok", data)
}

func Created(c *fiber.Ctx, data interface{}) error {
	return JSON(c, fiber.StatusCreated, "created", data)
}

func Error(c *fiber.Ctx, status int, message string) error {
	return JSON(c, status, message, nil)
}
