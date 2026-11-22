# ai-sessions

Case boyunca Claude ile yapılan çalışma oturumlarının export'ları. Kayıtların amacı ne kadar AI kullanıldığını değil, problemin nasıl ele alındığını ve AI çıktısının nasıl doğrulandığını göstermek.

## Oturumlar

| Tarih | Dosya | Kapsam |
|---|---|---|
| 2026-11-22 | `2026-11-22-gun1-7.md` (export eklenecek) | Gün 1-7: iskelet → segment → streaming içerik → onay → worker → pano → teslim hazırlığı |

## AI çıktısı nasıl doğrulandı

Her gün sonunda kriter testlerle ve çalışan sistemde kontrol edildi; yalnızca "derleniyor" kabul edilmedi.

| Alan | Doğrulama |
|---|---|
| Segment | Gerçek Postgres'te garanti kohort (450); 21 geçersiz çıktı türü; injection çıktıları; tarayıcıda injection brief'i |
| İçerik | Render sonrası limit sınırında testler (160 şablon + en uzun ad = 161); tarayıcıda "81 / 80" hatası; nginx arkasında akışın zaman damgalı ölçümü |
| Parser | Her byte bölme pozisyonu + 500 rastgele bölme, `encoding/json` ile karşılaştırma |
| Onay | 5 eşzamanlı onay; HTTP üzerinden 3 paralel istek (1×200, 2×409) |
| Worker | Q-01..06 gerçek Postgres'e karşı; uçtan uca test; compose'ta 2.070 teslimat ve gönderim ortasında `SIGKILL` (provider kabul = DB sent) |

## AI çıktısında bulunup düzeltilenler

Hepsi testler, ölçüm ya da kod incelemesiyle yakalandı.

| Ne | Nasıl bulundu | Düzeltme |
|---|---|---|
| Parser: eşsiz surrogate'ten sonra gelen escape (`\ud83d\n`) düz metin yazılıyordu | Kod incelemesi; `encoding/json` ile karşılaştıran test eklendi | Byte escape harfi olarak yeniden işleniyor |
| Üretim servisinde uzunluk kontrolüne boş sınır geçen geçici yer tutucu | Kod incelemesi | Gerçek sınırlar açıkça parametre |
| Onay ekranı tanımlanmadan önce bir değişkene erişiyordu | Vitest | Tanım sırası düzeltildi |
| Limiter HTTP'den önceki DB sorgusunun önünde bekliyordu; 1 s'de 105 başlangıç | Uçtan uca test (iki taraftan ölçüm) | Limiter HTTP çağrısının hemen önüne taşındı |
| Tam 100/s dışarıdan 101 ölçülüyordu (fencepost + scheduler gecikmesi) | Uçtan uca test, tekrarlı ölçüm | %3 açık güvenlik payı, ADR-8 |
| Mock LLM "İstanbullulara" yazımında şehri kaçırıyordu (450 yerine 971) | Compose'ta HTTP smoke test | Ek tanıyan sezgi + test |
| Mock JSON alan sırası alfabetikti; email gövdesi SMS'ten önce akıyordu | Tarayıcıda gözlem | Şema sırası korunuyor |
| BLUEPRINT §4.4'teki "en uzun adla render eşdeğerdir" iddiası | Akıl yürütme | Üst sınır olduğu not edildi; onayda alıcı bazlı kesin kontrol (ADR-11) |
| Testlerdeki yanlış beklentiler (ör. sınır dışı örnek, farklı gövdeyle tekrar) | Başarısız testin incelenmesi | Kod değil test düzeltildi; neden yazıldı |

## Export ve temizlik

1. Claude Code oturumunda `/export` çalıştırın ve dosyayı bu klasöre kaydedin.
2. Göndermeden önce kişisel bilgileri temizleyin: ev dizini yolları, email, kullanıcı adı. Örnek:

```bash
sed -i '' -e "s|$HOME|~|g" -e 's/[A-Za-z0-9._%+-]*@[A-Za-z0-9.-]*\.[a-z]\{2,\}/<email>/g' ai-sessions/*.md
```

3. `grep -rn "sk-ant-" ai-sessions/` boş dönmeli. Repoda API key yoktur; `.env` commit edilmez.
