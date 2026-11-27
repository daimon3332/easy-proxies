# Orca Proxy

**Aboneliklerinizi yönetin, proxy düğümlerini test edin ve tek bir yerel ağ geçidinden kullanın.**

Orca Proxy; farklı proxy kaynaklarını içe aktaran, bağlantıları otomatik test eden ve çalışan düğümleri yerel HTTP/SOCKS portları üzerinden sunan, sing-box tabanlı bir proxy yöneticisidir.

## Özellikler

- Abonelik URL'leri, URI listeleri, Base64, Clash/Mihomo YAML ve düz proxy listelerini içe aktarma
- Eşzamanlı bağlantı testleri, havuz yönetimi ve otomatik yenileme
- Her proxy için ayrı yerel port veya paylaşımlı havuz modu
- Ülke tespiti, hedef site kontrolleri ve zincir proxy profilleri
- Yedekleme, dışa aktarma ve Türkçe, koyu temalı WebUI

## Hızlı başlangıç

1. [Releases](https://github.com/kasimisik/orca-proxies/releases) sayfasından işletim sisteminize uygun paketi indirin.
2. `config.example.yaml` dosyasını `config.yaml` olarak kopyalayıp ayarları düzenleyin.
3. Uygulamayı başlatın:

```bash
./easy_proxies -config config.yaml
```

Web arayüzü varsayılan olarak [http://127.0.0.1:9091](http://127.0.0.1:9091) adresinde açılır.

Ayrıntılı kurulum ve yapılandırma için [Kullanım Kılavuzu](docs/USER_GUIDE.md) sayfasına bakın.

## Desteklenen protokoller

VLESS, VMess, Trojan, Shadowsocks, ShadowsocksR, Hysteria, Hysteria2, TUIC, AnyTLS, HTTP/HTTPS, SOCKS4 ve SOCKS5. Kullanılabilirlik derleme seçeneklerine ve sing-box sürümüne bağlıdır.

## Kaynak ve lisans

Orca Proxy, [daimon3332/easy-proxies](https://github.com/daimon3332/easy-proxies) projesinin topluluk odaklı bir fork'udur. Üst proje ve ilgili bileşenlerin atıfları korunmuştur.

[MIT Lisansı](LICENSE)
