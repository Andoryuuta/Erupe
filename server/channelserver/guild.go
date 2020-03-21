package channelserver

import (
	"database/sql"
	"fmt"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"time"
)

type Guild struct {
	ID          uint32
	Name        string
	MainMotto   string
	CreatedAt   time.Time
	MemberCount uint16
	Leader      *GuildMember
	RP          uint32
}

type GuildMember struct {
	GuildID     uint32    `db:"guild_id"`
	CharID      uint32    `db:"character_id"`
	JoinedAt    time.Time `db:"joined_at"`
	Name        string    `db:"name"`
	IsApplicant bool      `db:"is_applicant"`
	IsSubLeader bool      `db:"is_sub_leader"`
	OrderIndex  uint16    `db:"order_index"`
	LastLogin   uint32    `db:"last_login"`
}

const guildInfoSelectQuery = `
		SELECT g.id, g.name, g.rp, created_at, (
			SELECT count(1) FROM guild_characters gc WHERE gc.guild_id = g.id AND gc.is_applicant = false
		) AS member_count, leader_id, lc.name as leader_name, lgc.joined_at as leader_joined 
			FROM guilds g
					 JOIN guild_characters lgc ON lgc.character_id = leader_id
					 JOIN characters lc on leader_id = lc.id
`

func FindGuildsByName(s *Session, name string) ([]*Guild, error) {
	searchTerm := fmt.Sprintf("%%%s%%", name)

	rows, err := s.server.db.Query(fmt.Sprintf(`
		%s
		WHERE g.name ILIKE $1
	`, guildInfoSelectQuery), searchTerm)

	if err != nil {
		s.logger.Error("failed to find guilds for search term", zap.Error(err), zap.String("searchTerm", name))
		return nil, err
	}

	defer rows.Close()

	guilds := make([]*Guild, 0)

	for rows.Next() {
		guild, err := buildGuildObjectFromDbResult(rows, err, s)

		if err != nil {
			return nil, err
		}

		guilds = append(guilds, guild)
	}

	return guilds, nil
}

func GetGuildInfoByID(s *Session, guildID uint32) (*Guild, error) {
	rows, err := s.server.db.Query(fmt.Sprintf(`
		%s
		WHERE g.id = $1 
		LIMIT 1
	`, guildInfoSelectQuery), guildID)

	if err != nil {
		s.logger.Error("failed to retrieve guild", zap.Error(err), zap.Uint32("guildID", guildID))
		return nil, err
	}

	hasRow := rows.Next()

	if !hasRow {
		return nil, nil
	}

	return buildGuildObjectFromDbResult(rows, err, s)
}

func GetGuildInfoByCharacterId(s *Session, charID uint32) (*Guild, error) {
	rows, err := s.server.db.Query(fmt.Sprintf(`
		%s
		 JOIN guild_characters gc
			  ON g.id = gc.guild_id AND gc.character_id = $1
		LIMIT 1
	`, guildInfoSelectQuery), charID)

	if err != nil {
		s.logger.Error("failed to retrieve guild for character", zap.Error(err), zap.Uint32("charID", charID))
		return nil, err
	}

	defer rows.Close()

	hasRow := rows.Next()

	if !hasRow {
		return nil, nil
	}

	return buildGuildObjectFromDbResult(rows, err, s)
}

func GetGuildMembers(s *Session, guildID uint32, applicants bool) ([]*GuildMember, error) {
	rows, err := s.server.db.Queryx(`
		SELECT guild_id, joined_at, name, gc.character_id, gc.is_applicant, gc.is_sub_leader, gc.order_index, c.last_login
			FROM guild_characters gc
				JOIN characters c on gc.character_id = c.id
			WHERE guild_id = $1 AND is_applicant = $2
	`, guildID, applicants)

	if err != nil {
		s.logger.Error("failed to retrieve membership data for guild", zap.Error(err), zap.Uint32("guildID", guildID))
		return nil, err
	}

	defer rows.Close()

	members := make([]*GuildMember, 0)

	for rows.Next() {
		member, err := buildGuildMemberObjectFromDBResult(rows, err, s)

		if err != nil {
			return nil, err
		}

		members = append(members, member)
	}

	return members, nil
}

func buildGuildObjectFromDbResult(result *sql.Rows, err error, s *Session) (*Guild, error) {
	guild := &Guild{
		Leader: &GuildMember{},
	}

	err = result.Scan(
		&guild.ID, &guild.Name, &guild.RP, &guild.CreatedAt, &guild.MemberCount,
		&guild.Leader.CharID, &guild.Leader.Name, &guild.Leader.JoinedAt,
	)

	guild.Leader.GuildID = guild.ID
	guild.Leader.JoinedAt = guild.CreatedAt

	if err != nil {
		s.logger.Error("failed to retrieve guild data from database", zap.Error(err))
		return nil, err
	}

	return guild, nil
}

func GetCharacterGuildData(s *Session, charID uint32) (*GuildMember, error) {
	rows, err := s.server.db.Queryx(`
		SELECT guild_id, joined_at, name, character_id, gc.is_applicant, gc.is_sub_leader, gc.order_index, c.last_login
			FROM guild_characters gc
				JOIN characters c on gc.character_id = c.id
			WHERE character_id=$1
		LIMIT 1
	`, charID)

	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to retrieve membership data for character '%d'", charID))
		return nil, err
	}

	defer rows.Close()

	hasRow := rows.Next()

	if !hasRow {
		return nil, nil
	}

	return buildGuildMemberObjectFromDBResult(rows, err, s)
}

func (guild *Guild) Apply(s *Session, charID uint32) error {
	_, err := s.server.db.Exec(`
		INSERT INTO guild_characters (guild_id, character_id, is_applicant, order_index)
		VALUES ($1, $2, true, (SELECT MAX(order_index) + 1 FROM guild_characters WHERE guild_id = $1))
	`, guild.ID, charID)

	if err != nil {
		s.logger.Error(
			"failed to add applicant to guild",
			zap.Error(err),
			zap.Uint32("guildID", guild.ID),
			zap.Uint32("charID", charID),
		)
		return err
	}

	return nil
}

func (guild *Guild) Disband(s *Session) error {
	transaction, err := s.server.db.Begin()

	if err != nil {
		s.logger.Error("failed to begin transaction", zap.Error(err))
		return err
	}

	_, err = transaction.Exec("DELETE FROM guild_characters WHERE guild_id = $1", guild.ID)

	if err != nil {
		s.logger.Error("failed to remove guild characters", zap.Error(err), zap.Uint32("guildId", guild.ID))
		rollbackTransaction(s, transaction)
		return err
	}

	_, err = transaction.Exec("DELETE FROM guilds WHERE id = $1", guild.ID)

	if err != nil {
		s.logger.Error("failed to remove guild", zap.Error(err), zap.Uint32("guildID", guild.ID))
		rollbackTransaction(s, transaction)
		return err
	}

	err = transaction.Commit()

	if err != nil {
		s.logger.Error("failed to commit transaction", zap.Error(err))
		return err
	}

	s.logger.Info("Character disbanded guild", zap.Uint32("charID", s.charID), zap.Uint32("guildID", guild.ID))

	return nil
}

func (guild *Guild) RemoveCharacter(s *Session, charID uint32) error {
	_, err := s.server.db.Exec("DELETE FROM guild_characters WHERE character_id=$1", charID)

	if err != nil {
		s.logger.Error(
			"failed to remove character from guild",
			zap.Error(err),
			zap.Uint32("charID", charID),
			zap.Uint32("guildID", guild.ID),
		)

		return err
	}

	return nil
}

func (guild *Guild) AcceptCharacter(s *Session, charID uint32) error {
	_, err := s.server.db.Exec(`UPDATE guild_characters SET is_applicant = false WHERE character_id = $1`, charID)

	if err != nil {
		s.logger.Error("failed to accept character's guild application", zap.Error(err))
		return err
	}

	return nil
}

func (guild *Guild) ArrangeCharacters(s *Session, charIDs []uint32) error {
	transaction, err := s.server.db.Begin()

	if err != nil {
		s.logger.Error("failed to start db transaction", zap.Error(err))
		return err
	}

	for i, id := range charIDs {
		_, err := transaction.Exec("UPDATE guild_characters SET order_index = $1 WHERE character_id = $2", 2+i, id)

		if err != nil {
			err = transaction.Rollback()

			if err != nil {
				s.logger.Error("failed to rollback db transaction", zap.Error(err))
			}

			return err
		}
	}

	err = transaction.Commit()

	if err != nil {
		s.logger.Error("failed to commit db transaction", zap.Error(err))
		return err
	}

	return nil
}

func buildGuildMemberObjectFromDBResult(rows *sqlx.Rows, err error, s *Session) (*GuildMember, error) {
	memberData := &GuildMember{}

	err = rows.StructScan(&memberData)

	if err != nil {
		s.logger.Error("failed to retrieve guild data from database", zap.Error(err))
		return nil, err
	}

	return memberData, nil
}

func CreateGuild(s *Session, guildName string) (int32, error) {
	transaction, err := s.server.db.Begin()

	if err != nil {
		s.logger.Error("failed to start db transaction", zap.Error(err))
		return 0, err
	}

	guildResult, err := transaction.Query(
		"INSERT INTO guilds (name, leader_id) VALUES ($1, $2) RETURNING id",
		guildName, s.charID,
	)

	if err != nil {
		s.logger.Error("failed to create guild", zap.Error(err))
		rollbackTransaction(s, transaction)
		return 0, err
	}

	var guildId int32

	guildResult.Next()

	err = guildResult.Scan(&guildId)

	if err != nil {
		s.logger.Error("failed to retrieve guild ID", zap.Error(err))
		rollbackTransaction(s, transaction)
		return 0, err
	}

	err = guildResult.Close()

	if err != nil {
		s.logger.Error("failed to finalise query", zap.Error(err))
		rollbackTransaction(s, transaction)
		return 0, err
	}

	_, err = transaction.Exec(`
		INSERT INTO guild_characters (guild_id, character_id)
		VALUES ($1, $2)
	`, guildId, s.charID)

	if err != nil {
		s.logger.Error("failed to add character to guild", zap.Error(err))
		rollbackTransaction(s, transaction)
		return 0, err
	}

	err = transaction.Commit()

	if err != nil {
		s.logger.Error("failed to commit guild creation", zap.Error(err))
		return 0, err
	}

	return guildId, nil
}

func rollbackTransaction(s *Session, transaction *sql.Tx) {
	err := transaction.Rollback()

	if err != nil {
		s.logger.Error("failed to rollback transaction", zap.Error(err))
	}
}
