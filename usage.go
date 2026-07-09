package sconf

import (
	"reflect"

	"github.com/dvislobokov/sconf/bind"
)

// UsageEntry описывает один конфигурационный ключ (для программного доступа).
type UsageEntry = bind.Entry

// Describe возвращает список конфигурационных ключей типа T: путь, тип,
// значение по умолчанию (тег default), допустимые значения (тег enum) и
// описание (тег description или usage).
func Describe[T any]() []UsageEntry {
	return bind.Describe(typeOf[T]())
}

// Usage генерирует человекочитаемую справку по типу T на основе его полей и
// тегов. Ключи выводятся в форме командной строки (--section:key).
//
//	type Settings struct {
//	    Host string `description:"listen host" default:"0.0.0.0"`
//	    Mode string `enum:"dev,prod" default:"dev" usage:"run mode"`
//	}
//	fmt.Print(sconf.Usage[Settings]())
func Usage[T any]() string {
	return bind.Usage(typeOf[T]())
}

// HelpRequested сообщает, содержат ли аргументы флаг справки
// (-h, --h, -help, --help, -?, /?, /help).
func HelpRequested(args []string) bool {
	for _, a := range args {
		switch a {
		case "-h", "--h", "-help", "--help", "-?", "/?", "/help", "/h":
			return true
		}
	}
	return false
}

func typeOf[T any]() reflect.Type {
	var zero T
	if t := reflect.TypeOf(zero); t != nil {
		return t
	}
	// T — интерфейсный/указательный тип с нулевым значением nil.
	return reflect.TypeOf((*T)(nil)).Elem()
}
