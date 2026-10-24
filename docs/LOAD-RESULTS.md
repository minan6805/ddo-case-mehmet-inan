# SLA yük testi — 24 Ekim 2026

Komut: `make load`. k6 0.56.0, yerel Docker Compose ağı. 10 eşzamanlı sanal kullanıcı, 20 saniye, döngüler arasında 50 ms bekleme.

Hedef: oturum doğrulaması ve PostgreSQL kullanıcı/oturum okumasını içeren `POST /sla/preview`. Sabit küçük tarihçe için cuma akşamından pazartesiye doğru hedef hesaplanır. Her istekte HTTP 200 ve doğru tarih denetlenir. Kurulum/temizlikte uzman girişi ve çıkışı yapılır; token/parola rapora yazılmaz.

| Ölçüm | Sonuç |
|---|---|
| Hesaplama isteği | 3.930 |
| Toplam HTTP isteği (giriş/çıkış dahil) | 3.932 |
| HTTP istek hızı | 195,94/s |
| p95 yanıt süresi | 0,62544 ms |
| Ortalama yanıt süresi | 0,46267 ms |
| Hata | %0 (0/3.932) |
| Kontrol başarısı | %100 (7.860/7.860) |
| Eşikler | p95 <250 ms, hata <%1, kontrol >%99: PASS |

Bunlar bu makinedeki kısa ve küçük senaryonun ölçümleridir. Eşzamanlı takvim düzenlemesini, büyük tarihçeleri, kuyruk taramasını, soğuk başlangıcı veya tüm uygulamanın kapasitesini ölçmez. Üretim kapasitesi garantisi değildir. Başka donanımda sonuç değişebilir.

Ölçeği değiştirmek için örnek: `LOAD_VUS=30 LOAD_DURATION=30s make load`. Varsayılan 20 saniyelik yük testi `make verify` içinde çalışmaz.
