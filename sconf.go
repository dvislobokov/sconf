// Package sconf — конфигурационная библиотека для Go в духе
// Microsoft.Extensions.Configuration (ASP.NET Core), без зависимости от viper.
//
// Все источники сводятся к плоской модели "путь -> строка" с разделителем ":".
// Слои мержатся по порядку (последний выигрывает per key), после чего любая
// структура биндится единообразно из любого источника — включая массивы
// объектов из переменных среды (MYAPP_SERVERS__0__HOST → servers:0:host).
package sconf

import (
	"github.com/dvislobokov/sconf/bind"
	"github.com/dvislobokov/sconf/provider"
)

// ErrBindType возвращается (через %w), когда значение нельзя привести к типу.
var ErrBindType = bind.ErrBindType

// ErrEnum возвращается (через %w), когда значение не входит в список enum.
var ErrEnum = bind.ErrEnum

// Unmarshaler — тип, умеющий разобрать своё строковое представление сам.
type Unmarshaler = bind.Unmarshaler

// Validator — тип, валидирующий себя после бинда.
type Validator = bind.Validator

// FileOption настраивает файловый источник (Optional, Wait, PollInterval).
type FileOption = provider.FileOption

// Опции файловых источников, ре-экспортированные для эргономики.
var (
	Optional     = provider.Optional
	Wait         = provider.Wait
	PollInterval = provider.PollInterval
)
