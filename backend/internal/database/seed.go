package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// SeedDemo creates missing accounts only. Restarting never resets existing passwords.
func SeedDemo(ctx context.Context, pool *pgxpool.Pool, password string) error {
	if len(password) < 12 || len(password) > 72 {
		return fmt.Errorf("demo password must contain 12 to 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7420181)`); err != nil {
		return err
	}
	accounts := []struct{ email, name, role string }{
		{"calisan@destekmasa.local", "Demo Çalışan", "employee"},
		{"uzman@destekmasa.local", "Demo Destek Uzmanı", "specialist"},
		{"uzman2@destekmasa.local", "Demo Destek Uzmanı 2", "specialist"},
		{"uzman3@destekmasa.local", "Demo Destek Uzmanı 3", "specialist"},
		{"lider@destekmasa.local", "Demo Takım Lideri", "team_lead"},
		{"varlik@destekmasa.local", "Demo Varlık Yöneticisi", "asset_manager"},
		{"admin@destekmasa.local", "Demo Yönetici", "admin"},
	}
	for _, account := range accounts {
		if _, err = tx.Exec(ctx, `INSERT INTO users(email, display_name, role_code, password_hash)
            VALUES ($1,$2,$3,$4) ON CONFLICT (email) DO NOTHING`,
			account.email, account.name, account.role, string(hash)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(7420182)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7420183)`); err != nil {
		return err
	}
	profiles := []struct {
		email  string
		skills map[string]int
	}{
		{"uzman@destekmasa.local", map[string]int{"general": 5, "network": 5, "software": 4}},
		{"uzman2@destekmasa.local", map[string]int{"general": 5, "network": 4, "hardware": 5}},
		{"uzman3@destekmasa.local", map[string]int{"general": 4, "hardware": 4, "software": 5}},
	}
	for _, profile := range profiles {
		var id int64
		if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE email=$1 AND role_code='specialist'`, profile.email).Scan(&id); err != nil {
			return err
		}
		command, err := tx.Exec(ctx, `INSERT INTO expert_profiles(user_id) VALUES($1) ON CONFLICT DO NOTHING`, id)
		if err != nil {
			return err
		}
		// Preserve edited skills/shifts/capacity when seed runs again.
		if command.RowsAffected() == 0 {
			continue
		}
		for code, level := range profile.skills {
			if _, err = tx.Exec(ctx, `INSERT INTO expert_skills(expert_id,skill_code,level) VALUES($1,$2,$3)`, id, code, level); err != nil {
				return err
			}
		}
	}

	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7420184)`); err != nil {
		return err
	}
	marker, err := tx.Exec(ctx, `INSERT INTO asset_seed_state(name) VALUES('baseline-v1') ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	if marker.RowsAffected() > 0 {
		_, err = tx.Exec(ctx, `INSERT INTO assets(code,name,kind,state,owner_id) SELECT v.code,v.name,v.kind,'active',u.id FROM (VALUES
   ('DEMO-NET-01','Kurum ağı','network'),('DEMO-SRV-01','Uygulama sunucusu','server'),('DEMO-SVC-01','Kimlik servisi','service'),('DEMO-APP-01','Çalışan portalı','application')
  ) AS v(code,name,kind) CROSS JOIN users u WHERE u.email='varlik@destekmasa.local' ON CONFLICT(code) DO NOTHING`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO asset_dependencies(provider_id,dependent_id) SELECT a.id,b.id FROM (VALUES
  ('DEMO-NET-01','DEMO-SRV-01'),('DEMO-SRV-01','DEMO-SVC-01'),('DEMO-SVC-01','DEMO-APP-01')) AS e(provider,dependent) JOIN assets a ON a.code=e.provider JOIN assets b ON b.code=e.dependent ON CONFLICT DO NOTHING`)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE asset_graph_revision SET version=version+1`); err != nil {
			return err
		}
	}

	marker, err = tx.Exec(ctx, `INSERT INTO knowledge_seed_state(name) VALUES('baseline-v1') ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	if marker.RowsAffected() > 0 {
		_, err = tx.Exec(ctx, `INSERT INTO knowledge_articles(title,body,category_code,status,author_id)
   SELECT v.title,v.body,v.category,'published',u.id FROM (VALUES
    ('VPN bağlantısı için ilk kontroller','1. İnternet bağlantınızı kontrol edin.
2. Kurumun VPN uygulamasını açın ve sunucu adresini doğrulayın.
3. Hata sürerse hata mesajıyla bir ağ destek talebi oluşturun. Parolanızı paylaşmayın.','network'),
    ('Yazıcı çıktı vermiyorsa','1. Yazıcının açık ve bağlı olduğunu kontrol edin.
2. Doğru yazıcıyı seçtiğinizden emin olun.
3. Kağıt ve ekrandaki hata uyarılarını kontrol edin. Sorun sürerse donanım talebi açın.','hardware'),
    ('Destek talebi nasıl hazırlanır?','Sorunun başladığı zamanı, gördüğünüz hata mesajını ve denediğiniz adımları yazın. Uygun kategoriyi seçin; parola veya kişisel gizli bilgi eklemeyin.','general')
   ) AS v(title,body,category) CROSS JOIN users u WHERE u.email='lider@destekmasa.local'`)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
