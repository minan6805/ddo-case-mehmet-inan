# İndeks ölçümleri

26 Ekim 2026 tarihinde PostgreSQL 16.15 üzerinde ölçüldü. Aynı makinedeki ayrı `destekmasa-index-proof` Compose projesi kullanıldı; uygulamanın veritabanı veya indeksleri değiştirilmedi. Şema gerçek migrasyonlarla kuruldu. 1000 sentetik çalışan ve 200.000 sentetik talep üretildi; talepler dört kategoriye, beş duruma ve 365 güne dağıtıldı.

## Tekrar çalıştırma

```bash
make measure-indexes > /tmp/destekmasa-index-results.json
```

Docker, Make ve Python 3 gerekir. Betik yalnız kendi geçici PostgreSQL konteynerini başlatır; sonuçta bu projeyi temizler. PostgreSQL verisi tmpfs üzerindedir. Boş olmayan ölçüm veritabanını yeniden kullanmayı reddeder. Ana uygulamanın çalışması gerekmez.

Talep listeleme ve rapor özeti SELECT ifadeleri Go kaynaklarından okunur; uygulamanın sorgusu yerine sadeleştirilmiş bir örnek kullanılmaz. Gerçek bir çağrının parametreleri sabit değerlerle doldurulur; PostgreSQL bunları planlarken sabit koşulları sadeleştirebilir. Hazırlanmış sorguların generic planları bu ölçüme dahil değildir.

Her durum için üç ısınma ve yedi `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` çalıştırması yapılır. Önce indeksli, sonra yalnız incelenen indeks kaldırılmış durum ölçülür. Diğer bütün indeksler ve sorgu planlayıcısının varsayılan ayarları korunur. Ardından kaldırılan indeks yeniden oluşturulur. Tablo için VACUUM/ANALYZE yapılır. Raporlanan süre, yedi execution time değerinin medyanıdır; bağlantı kurma ve Python/Docker süreç süresi dahil değildir.

## Sonuçlar

| Sorgu | İndeks | İndeksli medyan | Bu indeks olmadan | Yaklaşık oran |
|---|---|---:|---:|---:|
| Çalışanın ikinci talep sayfası | `tickets_requester_id_desc_idx` | 0,173 ms | 0,977 ms | 5,6× |
| Bir günlük, kategori filtreli rapor özeti | `tickets_created_category_idx` | 0,551 ms | 5,064 ms | 9,2× |

Tam SELECT ifadeleri, yedi ölçümün tamamı ve medyan ölçüme ait tam plan: [INDEX-RESULTS.json](INDEX-RESULTS.json).

### Talep listesi

Koşullar: `requester_id=42`, `id<180000`, boş durum filtresi, `queue=all`, `LIMIT 21`. Sorgu talep alanlarıyla birlikte talep eden/atanan adlarını da getirir.

İndeksli planda `(requester_id, id DESC)` indeksi doğrudan sıralı ilk 21 kaydı getirir. Üst düğümün shared hit sayısı 30'dur. Bu indeks kaldırıldığında planlayıcı mevcut `tickets_requester_created_idx` indeksini kullanır; 180 aday kaydı getirip ID sırasına göre sıralar. Üst düğümün shared hit sayısı 210'a çıkar. Bu, alternatif indekslerin hâlâ faydalı olduğunu; ID tabanlı sayfalama indeksinin sıralama işini kaldırdığını gösterir.

### Rapor özeti

Koşullar: İstanbul saatinde 1 Ekim 2026 dahil, 2 Ekim hariç; `category=network`. Sorgu toplam, aktif, atanmamış aktif, çözülmüş ve kapalı sayıları üretir. 137 talep bu koşullara uyar.

İndeksli planda `(created_at, category_code)` üzerinden Bitmap Index Scan ve Bitmap Heap Scan kullanılır; üst düğümün shared hit sayısı 144'tür. Bu indeks kaldırılınca PostgreSQL mevcut `tickets_requester_created_idx` üzerinde alternatif Bitmap Index Scan seçer; 548 tarih adayını kategoriye göre süzer. Üst düğümün shared hit sayısı 1764 olur. İndeks olmadan da sorgu tam tablo taraması yapmak zorunda değildir; sonuç gerçek alternatif planla karşılaştırılmıştır.

## Kapsam

Bu bir sıcak önbellek ve tek istemci sorgu ölçümüdür. Sentetik dağılım, seçilen tarih/çalışan, donanım ve PostgreSQL istatistikleri sonucu etkiler. Oranlar bütün veri dağılımlarında veya üretim yüklerinde aynı kalacağı anlamına gelmez. İndekslerin yazma maliyeti, cold cache, eşzamanlı trafik ve bütün rapor ekranının toplam yanıt süresi burada ölçülmez. SLA yük testi ayrı olarak [LOAD-RESULTS.md](LOAD-RESULTS.md) içindedir.
