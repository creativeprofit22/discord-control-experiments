package reconcile

import (
	"fmt"
	"sort"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const discordCategoryType domain.ChannelType = 4

type SnapshotInput struct {
	Guild               domain.Guild
	ApplicationID       domain.ApplicationID
	Channels            []domain.Channel
	Roles               []domain.Role
	Webhooks            []domain.Webhook
	ApplicationCommands []domain.ApplicationCommand
	Onboarding          *domain.Onboarding
	OnboardingSupport   domain.SupportStatus
	Bindings            []domain.ResourceBinding
	CapturedAt          time.Time
}

func NormalizeSnapshot(input SnapshotInput) (domain.GuildSnapshot, error) {
	if input.Guild.ID == "" {
		return domain.GuildSnapshot{}, fmt.Errorf("snapshot guild ID is required")
	}
	bindingsByRemote := make(map[domain.Snowflake]domain.ResourceBinding, len(input.Bindings))
	for _, binding := range input.Bindings {
		if binding.RemoteID == "" || binding.Ref.LogicalID == "" {
			return domain.GuildSnapshot{}, fmt.Errorf("invalid snapshot binding")
		}
		if _, duplicate := bindingsByRemote[binding.RemoteID]; duplicate {
			return domain.GuildSnapshot{}, fmt.Errorf("duplicate binding for remote ID %s", binding.RemoteID)
		}
		bindingsByRemote[binding.RemoteID] = binding
	}

	channels := append([]domain.Channel(nil), input.Channels...)
	roles := append([]domain.Role(nil), input.Roles...)
	webhooks := append([]domain.Webhook(nil), input.Webhooks...)
	commands := append([]domain.ApplicationCommand(nil), input.ApplicationCommands...)
	if err := validateObservedGuild(input.Guild.ID, input.ApplicationID, channels, webhooks, commands, input.Onboarding); err != nil {
		return domain.GuildSnapshot{}, err
	}

	sort.SliceStable(roles, func(i, j int) bool {
		if roles[i].Position != roles[j].Position {
			return roles[i].Position < roles[j].Position
		}
		return roles[i].ID < roles[j].ID
	})
	categoryPosition := make(map[domain.ChannelID]int)
	for _, channel := range channels {
		if channel.Type == discordCategoryType {
			categoryPosition[channel.ID] = channel.Position
		}
	}
	sort.SliceStable(channels, func(i, j int) bool {
		leftParent, rightParent := parentOrder(channels[i], categoryPosition), parentOrder(channels[j], categoryPosition)
		if leftParent != rightParent {
			return leftParent < rightParent
		}
		if channels[i].Position != channels[j].Position {
			return channels[i].Position < channels[j].Position
		}
		return channels[i].ID < channels[j].ID
	})
	sort.SliceStable(webhooks, func(i, j int) bool { return webhooks[i].ID < webhooks[j].ID })
	sort.SliceStable(commands, func(i, j int) bool {
		if commands[i].Type != commands[j].Type {
			return commands[i].Type < commands[j].Type
		}
		if commands[i].Name != commands[j].Name {
			return commands[i].Name < commands[j].Name
		}
		return commands[i].ID < commands[j].ID
	})

	snapshot := domain.GuildSnapshot{
		Metadata: domain.SnapshotMetadata{SchemaVersion: domain.SnapshotSchemaV1, GuildID: input.Guild.ID, ApplicationID: input.ApplicationID, CapturedAt: input.CapturedAt},
		Guild:    input.Guild, Categories: []domain.SnapshotResource{}, Channels: []domain.SnapshotResource{}, Roles: []domain.SnapshotResource{}, Webhooks: []domain.SnapshotResource{}, ApplicationCommands: []domain.SnapshotResource{},
		Onboarding: input.Onboarding, Capabilities: []domain.SnapshotCapability{}, Bindings: append([]domain.ResourceBinding(nil), input.Bindings...),
		ObservedChannels: channels, ObservedRoles: roles, ObservedWebhooks: webhooks, ObservedCommands: commands,
	}
	channelRefs := make(map[domain.ChannelID]domain.StableResourceRef)
	for index, channel := range channels {
		kind := domain.ResourceChannel
		if channel.Type == discordCategoryType {
			kind = domain.ResourceCategory
		}
		resource, err := snapshotChannel(channel, kind, index, bindingsByRemote[domain.Snowflake(channel.ID)])
		if err != nil {
			return domain.GuildSnapshot{}, err
		}
		channelRefs[channel.ID] = resource.Ref
		if kind == domain.ResourceCategory {
			snapshot.Categories = append(snapshot.Categories, resource)
		} else {
			snapshot.Channels = append(snapshot.Channels, resource)
		}
	}
	for index := range snapshot.Channels {
		observed := findChannel(channels, domain.ChannelID(snapshot.Channels[index].RemoteID))
		if observed != nil && observed.ParentID != nil {
			parent := channelRefs[*observed.ParentID]
			snapshot.Channels[index].ParentRef = &parent
		}
	}
	for index, role := range roles {
		resource, err := snapshotRole(role, index, bindingsByRemote[domain.Snowflake(role.ID)], role.ID == domain.RoleID(input.Guild.ID) || role.Managed)
		if err != nil {
			return domain.GuildSnapshot{}, err
		}
		snapshot.Roles = append(snapshot.Roles, resource)
	}
	for index, webhook := range webhooks {
		resource, err := snapshotWebhook(webhook, index, bindingsByRemote[domain.Snowflake(webhook.ID)])
		if err != nil {
			return domain.GuildSnapshot{}, err
		}
		snapshot.Webhooks = append(snapshot.Webhooks, resource)
	}
	for index, command := range commands {
		resource, err := snapshotCommand(command, index, bindingsByRemote[domain.Snowflake(command.ID)])
		if err != nil {
			return domain.GuildSnapshot{}, err
		}
		snapshot.ApplicationCommands = append(snapshot.ApplicationCommands, resource)
	}
	status := input.OnboardingSupport
	if status == "" {
		if input.Onboarding == nil {
			status = domain.SupportUnknown
		} else {
			status = domain.SupportAvailable
		}
	}
	snapshot.Capabilities = append(snapshot.Capabilities, domain.SnapshotCapability{Resource: domain.ResourceOnboarding, Status: status})
	sort.Slice(snapshot.Bindings, func(i, j int) bool { return refLess(snapshot.Bindings[i].Ref, snapshot.Bindings[j].Ref) })
	hash, err := SnapshotHash(snapshot)
	if err != nil {
		return domain.GuildSnapshot{}, err
	}
	snapshot.Metadata.CanonicalHash = hash
	return snapshot, nil
}

func SnapshotHash(snapshot domain.GuildSnapshot) (string, error) {
	copySnapshot := snapshot
	copySnapshot.Metadata.CapturedAt = time.Time{}
	copySnapshot.Metadata.CanonicalHash = ""
	return HashCanonical(copySnapshot)
}

func snapshotChannel(channel domain.Channel, kind domain.ResourceKind, order int, binding domain.ResourceBinding) (domain.SnapshotResource, error) {
	mutable := channel
	mutable.ID, mutable.GuildID, mutable.Position, mutable.ParentID = "", nil, 0, nil
	fingerprint, err := HashCanonical(mutable)
	if err != nil {
		return domain.SnapshotResource{}, err
	}
	name := ""
	if channel.Name != nil {
		name = *channel.Name
	}
	return makeSnapshotResource(kind, domain.Snowflake(channel.ID), name, int(channel.Type), order, fingerprint, binding, false,
		[]string{"name", "type", "parent_id", "position", "topic", "nsfw", "rate_limit_per_user", "permission_overwrites", "forum"}), nil
}

func snapshotRole(role domain.Role, order int, binding domain.ResourceBinding, protected bool) (domain.SnapshotResource, error) {
	mutable := role
	mutable.ID, mutable.Position, mutable.Managed = "", 0, false
	fingerprint, err := HashCanonical(mutable)
	if err != nil {
		return domain.SnapshotResource{}, err
	}
	return makeSnapshotResource(domain.ResourceRole, domain.Snowflake(role.ID), role.Name, 0, order, fingerprint, binding, protected,
		[]string{"name", "permissions", "position", "color", "hoist", "mentionable"}), nil
}

func snapshotWebhook(webhook domain.Webhook, order int, binding domain.ResourceBinding) (domain.SnapshotResource, error) {
	mutable := webhook
	mutable.ID, mutable.GuildID, mutable.ApplicationID = "", nil, nil
	fingerprint, err := HashCanonical(mutable)
	if err != nil {
		return domain.SnapshotResource{}, err
	}
	name := ""
	if webhook.Name != nil {
		name = *webhook.Name
	}
	protected := webhook.Type != 1 || webhook.ApplicationID != nil
	return makeSnapshotResource(domain.ResourceWebhook, domain.Snowflake(webhook.ID), name, webhook.Type, order, fingerprint, binding, protected, []string{"name", "channel_id"}), nil
}

func snapshotCommand(command domain.ApplicationCommand, order int, binding domain.ResourceBinding) (domain.SnapshotResource, error) {
	mutable := command
	mutable.ID, mutable.ApplicationID, mutable.GuildID, mutable.Version = "", "", nil, ""
	fingerprint, err := HashCanonical(mutable)
	if err != nil {
		return domain.SnapshotResource{}, err
	}
	return makeSnapshotResource(domain.ResourceApplicationCommand, domain.Snowflake(command.ID), command.Name, int(command.Type), order, fingerprint, binding, false,
		[]string{"type", "name", "description", "options", "default_member_permissions", "nsfw"}), nil
}

func makeSnapshotResource(kind domain.ResourceKind, remoteID domain.Snowflake, name string, resourceType, order int, fingerprint string, binding domain.ResourceBinding, protected bool, fields []string) domain.SnapshotResource {
	managed := binding.RemoteID != ""
	ref := binding.Ref
	if !managed {
		ref = domain.StableResourceRef{Kind: kind, LogicalID: "observed-" + string(remoteID)}
	}
	disposition := domain.FieldObserved
	if managed {
		disposition = domain.FieldManaged
	}
	snapshotFields := make([]domain.SnapshotField, 0, len(fields))
	for _, field := range fields {
		snapshotFields = append(snapshotFields, domain.SnapshotField{Name: field, Disposition: disposition})
	}
	return domain.SnapshotResource{Ref: ref, RemoteID: remoteID, Name: name, Type: resourceType, Order: order, Managed: managed, Protected: protected, Fingerprint: fingerprint, Fields: snapshotFields}
}

func validateObservedGuild(guildID domain.GuildID, applicationID domain.ApplicationID, channels []domain.Channel, webhooks []domain.Webhook, commands []domain.ApplicationCommand, onboarding *domain.Onboarding) error {
	for _, channel := range channels {
		if channel.GuildID != nil && *channel.GuildID != guildID {
			return fmt.Errorf("channel %s belongs to another guild", channel.ID)
		}
	}
	for _, webhook := range webhooks {
		if webhook.GuildID == nil || *webhook.GuildID != guildID {
			return fmt.Errorf("webhook %s does not belong to requested guild", webhook.ID)
		}
	}
	for _, command := range commands {
		if command.GuildID == nil || *command.GuildID != guildID {
			return fmt.Errorf("application command %s does not belong to requested guild", command.ID)
		}
		if applicationID != "" && command.ApplicationID != applicationID {
			return fmt.Errorf("application command %s belongs to another application", command.ID)
		}
	}
	if onboarding != nil && onboarding.GuildID != guildID {
		return fmt.Errorf("onboarding belongs to another guild")
	}
	return nil
}

func parentOrder(channel domain.Channel, categories map[domain.ChannelID]int) int {
	if channel.Type == discordCategoryType {
		return channel.Position * 10000
	}
	if channel.ParentID == nil {
		return -1
	}
	return categories[*channel.ParentID] * 10000
}

func findChannel(channels []domain.Channel, id domain.ChannelID) *domain.Channel {
	for index := range channels {
		if channels[index].ID == id {
			return &channels[index]
		}
	}
	return nil
}
