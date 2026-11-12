<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="frontend/assets/brand/sintet-light.svg">
    <img src="frontend/assets/brand/sintet-dark.svg" alt="Sintet" width="210">
  </picture>
</p>

<p align="center"><strong>Лаунчер Minecraft Java Edition для Windows 10 и 11</strong></p>

<p align="center">
  <a href="https://sintet.xyz/#download"><strong>Скачать установщик</strong></a> ·
  <a href="https://github.com/zzoyn522-sys/Sintet/releases">Стабильные выпуски</a> ·
  <a href="https://discord.gg/KURVdKAUAy">Discord</a> ·
  <a href="https://t.me/sinted_launcher">Telegram</a>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-GPLv3-282828" alt="GPLv3"></a>
  <img src="https://img.shields.io/badge/Windows-x64-282828" alt="Windows x64">
</p>

![Главный экран Sintet с выбранной версией Minecraft](docs/screenshots/launcher.png)

## Возможности

- Vanilla, Fabric, Forge и NeoForge. Лаунчер подбирает Java и проверяет файлы перед запуском.
- Отдельные сборки и мастерская. Ищи моды, шейдеры, ресурс-паки и сборки в каталогах. В «Моём» можно выделить несколько карточек правой кнопкой и управлять ими сразу.
- Друзья, лобби и оверлей в Minecraft. Видимость оверлея можно отключить в настройках.
- Оформления с собственными фонами, шрифтами и фильтрами. Настройки темы можно экспортировать в `.sldcfg`.

## Кастомное оформление

<p align="center">
  <img src="docs/screenshots/custom-workshop.png" alt="Мастерская с пользовательским фоном и шрифтом" width="49%">
  <img src="docs/screenshots/custom-lobby.png" alt="Лобби с тем же пользовательским оформлением" width="49%">
</p>

<details>
<summary>Ещё скриншоты</summary>

![Выбор источника установки в мастерской](docs/screenshots/workshop.png)

![Настройки фильтров фона](docs/screenshots/appearance.png)

Скриншоты в этом блоке сняты с интерфейса лаунчера на демонстрационных данных.

</details>

## Исходники и обновления

| Компонент | Где смотреть |
| --- | --- |
| Установка Minecraft, Java и загрузчиков | [`internal/launcher`](internal/launcher) |
| Мастерская и интерфейс | [`internal/workshop`](internal/workshop), [`frontend`](frontend) |
| Оверлей и игровой мост | [`native`](native), [`bridge`](bridge) |
| Проверка и установка обновлений | [`updater`](updater), [`updates.go`](updates.go) |

GitHub Releases хранит копии стабильных сборок. Установленный лаунчер получает обновления через [стабильный канал Sintet](https://sintet-updates.zzoyn522.workers.dev/stable/latest.json). Манифест подписан Ed25519 и содержит SHA-256 и размер EXE. Лаунчер проверяет их перед установкой. Для ручной проверки EXE на GitHub в выпуске приложена контрольная сумма.

При включённом оверлее лаунчер передаёт DLL JVM через `-agentpath`. Игровой мост подключается через `-javaagent`. [Подробнее об оверлее](native/README.md).

## Сборка из исходников

На Windows x64 нужны Go 1.24+, JDK 17+, Visual Studio 2022 Build Tools с компонентами C++ и Windows SDK. Для запуска нужен Microsoft WebView2 Runtime.

```powershell
$version = (Get-Content VERSION -Raw).Trim()
cmd.exe /d /c native\build.bat
powershell.exe -NoProfile -ExecutionPolicy Bypass -File bridge/build.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File updater/build.ps1 -Version $version
go build -trimpath -tags desktop,production -ldflags "-s -w -H windowsgui -X main.launcherVersion=$version" -o Sintet-launcher.exe .
```

Запусти собранный `Sintet-launcher.exe`.

После сборки можно выполнить `go test ./...`. Для проверки JavaScript нужен Node.js 22+: `node --test tests/*.test.* _checks/workshop-sources.test.mjs`. [Сборка установщика](installer/README.md) · [Формат оформлений](SLDCFG.md).

## Лицензия

Исходный код Sintet распространяется по [GNU GPL версии 3](LICENSE). Сторонние библиотеки и ресурсы сохраняют собственные условия. Лицензии библиотек находятся в [`native/vendor`](native/vendor), сведения об отдельных значках – в [`frontend/assets`](frontend/assets).
