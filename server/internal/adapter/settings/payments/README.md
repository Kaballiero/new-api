# Адрес возврата платежей

PaymentReturnURL читает текущий system_setting.ServerAddress, удаляет конечные slash и добавляет переданный dashboard suffix. Billing notification adapter использует эту настройку без изменения текста/адреса уведомлений. Собственного cache, business policy и startup state нет.
