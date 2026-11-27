# ai-sessions

Case boyunca iki ana AI oturumuyla çalıştım. Mimari önerileri karşılaştırmak için Gemini ve ChatGPT'yi de kullandım.

| Oturum | Ne yapıldı | Bu klasörde |
|---|---|---|
| claude.ai sohbeti | Case analizi, Gemini ve ChatGPT planlarının karşılaştırılması, BLUEPRINT.md'nin hazırlanması, mimari kararların tartışılması ve araştırılması, gerçek Claude API testleri, teslim öncesi denetim | [oturum-ozeti.md](oturum-ozeti.md) (oturumun özetlenmiş hali) |
| Claude Code oturumu | BLUEPRINT'te 7 faza bölünen kodlamanın yapılması, testlerin yazılması ve çalıştırılması, teslim öncesi iki bağımsız kod incelemesi ve push | [claude-code-oturumu.md](claude-code-oturumu.md) (oturum export'u) |

### Planlama sohbetinin ham kaydı neden yok

Asıl kararların tartışıldığı ve araştırıldığı oturum claude.ai sohbeti. Bu sohbet CV'm, önceki bir mülakatın kaydı ve bir API key gibi çok kişisel detaylar içeriyor. Bu yüzden ham halini şu an paylaşamıyorum. Ham kayıtları görüşmede birlikte açabilirim.

Onun yerine oturumun özetlenmiş halini [oturum-ozeti.md](oturum-ozeti.md)'ye koydum. Her adımda AI'dan ne istediğimi, hangi çıktıları ve soruları aldığımı, hangi alternatifleri değerlendirdiğimi, nasıl bir aksiyon aldığımı ve kararı nasıl doğruladığımı gösteriyor. Amacım çalışmamı nasıl hazırladığımı ve AI çıktısını nasıl yönettiğimi göstermekti. Zayıf yanlarım da dahil.

### Claude Code kaydı hakkında

[claude-code-oturumu.md](claude-code-oturumu.md) oturumun export'undan üretildi; konuşma metni değiştirilmedi. Yalnızca ev dizini yolları, hesap adları, token'lar ve case dışı kişisel bilgiler maskelendi, oturumun ai-sessions hazırlığına ait son kısmı çıkarıldı. Bunlar dosyanın başında ve sonunda yazıyor.

Kayıtta sık geçen "Gün 2'ye geç", "Gün 3'e geç" mesajları BLUEPRINT'teki faz başlıklarına karşılık geliyor. Plan bu oturumdan önce claude.ai sohbetinde ayrıntılı olarak yapılmıştı; bu mesajlar "planın sıradaki fazını uygula" demekti. Repo ve dokümanlarda bu fazlar "Faz 1-7" olarak geçiyor.

Kısaca: kodun büyük kısmını Claude Code yazdı. Benim işim planı yapmak, kararları vermek ve çıktıyı doğrulamaktı. Kodlama fazlarında çoğunlukla Claude'un önerilerini kabul edip ilerledim; asıl doğrulamayı teslimden önce gerçek Claude API testleri, worker'ı gönderim ortasında öldürme testi ve iki bağımsız incelemeyle yaptım.

## AI çıktısı nasıl doğrulandı

Faz sonlarında Claude Code BLUEPRINT'teki kriter testlerini ve çalışan sistemi kontrol etti; teslimden önce bunların üstüne gerçek Claude testleri ve bağımsız incelemeler yapıldı ([oturum-ozeti.md](oturum-ozeti.md), adım 10-13).

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
| Placeholder'a Türkçe ek: `{{city}}'a` → "Bursa'a", `{{city}}'dan` → "İzmir'dan" (çoğu metinde) | Gerçek Claude ile 30 senaryoluk test | Validator kuralı + prompt `copy_v2` + frontend uyarısı (ADR-16); tekrar testte 0/7 |
| "İzmir'deki VIP müşterilere": VIP kuralı uygulanamadı ama metin "VIP müşterilerimize özel" diyordu | Gerçek Claude çıktısını okuyarak | Uygulanamayan kriterler içerik prompt'una ve onay diyaloğuna taşındı (ADR-17) |
| Gerçek Claude alanları alfabetik sırayla üretiyordu (önce email gövdesi) | Gerçek Claude testinde alan sırası ölçümü | Şema `properties` map yerine sıralı struct; SDK isteği üzerinden test |
| `make test`, araya `docker compose down` girince silinmiş ağa bağlı test DB yüzünden kırılıyordu | Teslim öncesi temiz ortam testi | `make test-go` önce eski test DB container'ını kaldırıyor |
| Türkçe ek kuralı dört mevcut testi bozdu; biri yanlış sebeple yeşil kalıyordu (alıcı bazlı kontrol yerine ek kuralına takılıyordu); "testler yeşil" iddiası doğrulanmamıştı | Bağımsız inceleme, `make test` | Test şablonları düzeltildi, o test hatanın gerçek kaynağını doğrulayacak şekilde sıkılaştırıldı |
| Sadakat kontrolünde link regex'i cümle sonundaki noktayı linke katıyordu (brief'teki linki "uydurma" sayardı) | Bağımsız inceleme, kenar durum testleri | Sondaki noktalama kırpılıyor; üç yazım için test |
| Toplu yorum temizliği bir yorumu noktayla başlar bıraktı; bir yorum README'de olmayan bir tabloya atıf yapıyordu | Bağımsız inceleme, kod okuma | Düzeltildi |
| BLUEPRINT §4.4'teki "en uzun adla render eşdeğerdir" iddiası | Akıl yürütme | Üst sınır olduğu not edildi; onayda alıcı bazlı kesin kontrol (ADR-11) |
| Testlerdeki yanlış beklentiler (ör. sınır dışı örnek, farklı gövdeyle tekrar) | Başarısız testin incelenmesi | Kod değil test düzeltildi; neden yazıldı |
