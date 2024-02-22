package handlers

import (
	"errors"
	"fmt"

	"github.com/capcom6/sms-gateway/internal/sms-gateway/models"
	"github.com/capcom6/sms-gateway/internal/sms-gateway/repositories"
	"github.com/capcom6/sms-gateway/internal/sms-gateway/services"
	"github.com/capcom6/sms-gateway/pkg/smsgateway"
	"github.com/capcom6/sms-gateway/pkg/types"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

const (
	route3rdPartyGetMessage = "3rdparty.get.message"
)

type ThirdPartyHandlerParams struct {
	fx.In

	AuthSvc     *services.AuthService
	MessagesSvc *services.MessagesService
	DevicesSvc  *services.DevicesService

	Logger    *zap.Logger
	Validator *validator.Validate
}

type thirdPartyHandler struct {
	Handler

	authSvc     *services.AuthService
	messagesSvc *services.MessagesService
	devicesSvc  *services.DevicesService
}

//	@Summary		Получить устройства
//	@Description	Возвращает все устройства пользователя
//	@Security		ApiAuth
//	@Tags			Пользователь, Устройства
//	@Produce		json
//	@Success		200	{object}	[]smsgateway.Device			"Состояние сообщения"
//	@Failure		401	{object}	smsgateway.ErrorResponse	"Ошибка авторизации"
//	@Failure		400	{object}	smsgateway.ErrorResponse	"Некорректный запрос"
//	@Failure		500	{object}	smsgateway.ErrorResponse	"Внутренняя ошибка сервера"
//	@Router			/3rdparty/v1/device [get]
//
// Получить устройства
func (h *thirdPartyHandler) getDevice(user models.User, c *fiber.Ctx) error {
	devices, err := h.devicesSvc.Select(user)
	if err != nil {
		return fmt.Errorf("can't select devices: %w", err)
	}

	response := make([]smsgateway.Device, 0, len(devices))

	for _, device := range devices {
		response = append(response, smsgateway.Device{
			ID:        device.ID,
			Name:      types.OrDefault[string](device.Name, ""),
			CreatedAt: device.CreatedAt,
			UpdatedAt: device.UpdatedAt,
			DeletedAt: device.DeletedAt,
			LastSeen:  device.LastSeen,
		})
	}

	return c.JSON(response)
}

//	@Summary		Поставить сообщение в очередь
//	@Description	Ставит сообщение в очередь на отправку. Если идентификатор не указан, то он будет сгенерирован автоматически
//	@Security		ApiAuth
//	@Tags			Пользователь, Сообщения
//	@Accept			json
//	@Produce		json
//	@Param			skipPhoneValidation	query		bool						false	"Пропустить проверку номеров телефона"
//	@Param			request				body		smsgateway.Message			true	"Сообщение"
//	@Success		202					{object}	smsgateway.MessageState		"Сообщение поставлено в очередь"
//	@Failure		401					{object}	smsgateway.ErrorResponse	"Ошибка авторизации"
//	@Failure		400					{object}	smsgateway.ErrorResponse	"Некорректный запрос"
//	@Failure		500					{object}	smsgateway.ErrorResponse	"Внутренняя ошибка сервера"
//	@Header			202					{string}	Location					"URL для получения состояния сообщения"
//	@Router			/3rdparty/v1/message [post]
//
// Поставить сообщение в очередь
func (h *thirdPartyHandler) postMessage(user models.User, c *fiber.Ctx) error {
	req := smsgateway.Message{}
	if err := h.BodyParserValidator(c, &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	skipPhoneValidation := c.QueryBool("skipPhoneValidation", false)

	devices, err := h.devicesSvc.Select(user)
	if err != nil {
		return fmt.Errorf("can't select devices: %w", err)
	}

	if len(devices) < 1 {
		return fiber.NewError(fiber.StatusBadRequest, "Нет ни одного устройства в учетной записи")
	}

	device := devices[0]
	state, err := h.messagesSvc.Enqeue(device, req, services.MessagesEnqueueOptions{SkipPhoneValidation: skipPhoneValidation})
	if err != nil {
		var err400 services.ErrValidation
		if errors.As(err, &err400) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}

		return err
	}

	location, err := c.GetRouteURL(route3rdPartyGetMessage, fiber.Map{
		"id": state.ID,
	})
	if err != nil {
		h.Logger.Error("Failed to get route URL", zap.String("route", route3rdPartyGetMessage), zap.Error(err))
	} else {
		c.Location(location)
	}

	return c.Status(fiber.StatusAccepted).JSON(state)
}

//	@Summary		Получить состояние сообщения
//	@Description	Возвращает состояние сообщения по его ID
//	@Security		ApiAuth
//	@Tags			Пользователь, Сообщения
//	@Produce		json
//	@Param			id	path		string						true	"ИД сообщения"
//	@Success		200	{object}	smsgateway.MessageState		"Состояние сообщения"
//	@Failure		401	{object}	smsgateway.ErrorResponse	"Ошибка авторизации"
//	@Failure		400	{object}	smsgateway.ErrorResponse	"Некорректный запрос"
//	@Failure		500	{object}	smsgateway.ErrorResponse	"Внутренняя ошибка сервера"
//	@Router			/3rdparty/v1/message [get]
//
// Получить состояние сообщения
func (h *thirdPartyHandler) getMessage(user models.User, c *fiber.Ctx) error {
	id := c.Params("id")

	state, err := h.messagesSvc.GetState(user, id)
	if err != nil {
		if errors.Is(err, repositories.ErrMessageNotFound) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}

		return err
	}

	return c.JSON(state)
}

func (h *thirdPartyHandler) authorize(handler func(models.User, *fiber.Ctx) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		username := c.Locals("username").(string)
		password := c.Locals("password").(string)

		user, err := h.authSvc.AuthorizeUser(username, password)
		if err != nil {
			h.Logger.Error("failed to authorize user", zap.Error(err))
			return fiber.ErrUnauthorized
		}

		return handler(user, c)
	}
}

func (h *thirdPartyHandler) Register(router fiber.Router) {
	router = router.Group("/3rdparty/v1")

	router.Use(basicauth.New(basicauth.Config{
		Authorizer: func(username string, password string) bool {
			return len(username) > 0 && len(password) > 0
		},
	}))

	router.Get("/device", h.authorize(h.getDevice))

	router.Post("/message", h.authorize(h.postMessage))
	router.Get("/message/:id", h.authorize(h.getMessage)).Name(route3rdPartyGetMessage)
}

func newThirdPartyHandler(params ThirdPartyHandlerParams) *thirdPartyHandler {
	return &thirdPartyHandler{
		Handler:     Handler{Logger: params.Logger.Named("ThirdPartyHandler"), Validator: params.Validator},
		authSvc:     params.AuthSvc,
		messagesSvc: params.MessagesSvc,
		devicesSvc:  params.DevicesSvc,
	}
}
