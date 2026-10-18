package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/8bit-warrior/Miniray/internal/amneziawg"
	"github.com/8bit-warrior/Miniray/internal/amneziawgnet"
	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/tuic"
	"github.com/8bit-warrior/Miniray/internal/xray"
)

type LocalDeps struct {
	APIPort        func() int
	SetNeedRestart func()
}

type Local struct {
	deps LocalDeps
	mu   sync.Mutex
}

func NewLocal(deps LocalDeps) *Local {
	return &Local{deps: deps}
}

func (l *Local) Name() string { return "local" }

func (l *Local) withAPI(fn func(api *xray.XrayAPI) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	port := l.deps.APIPort()
	if port <= 0 {
		return errors.New("local xray is not running")
	}
	var api xray.XrayAPI
	if err := api.Init(port); err != nil {
		return err
	}
	defer api.Close()
	return fn(&api)
}

func (l *Local) AddInbound(_ context.Context, ib *model.Inbound) error {
	
	if ib.Protocol == model.AmneziaWG {
		inst, ok := amneziawg.InstanceFromInbound(ib)
		if !ok {
			return nil
		}
		err := amneziawgnet.GetManager().Ensure(amneziawgnet.Desired{
			Instance: inst,
			Options: amneziawgnet.DeviceOptions{
				HeaderProtectionKey:    inst.Obfuscation.HeaderProtectionKey,
				ContentPaddingAddition: inst.Obfuscation.ContentPaddingAddition,
				RekeyAfterTime:         inst.Obfuscation.RekeyAfterTime,
				RekeyTimeout:           inst.Obfuscation.RekeyTimeout,
				RejectAfterTime:        inst.Obfuscation.RejectAfterTime,
				KeepaliveTimeout:       inst.Obfuscation.KeepaliveTimeout,
				MaxHandshakeAttempts:   inst.Obfuscation.MaxHandshakeAttempts,
				RandomTrailers:         inst.Obfuscation.RandomTrailers,
				DisableCookies:         inst.Obfuscation.DisableCookies,
			},
		})
		// A brand new inbound can be the first one to qualify for
		// injectAmneziawgnetSocks's Xray-side relay inbound (e.g. its first
		// valid peer). Ensure only updates the embedded Device -- flag Xray
		// for a resync so the relay actually gets created within the next
		// ApplyPendingRestart tick instead of only at the next full restart.
		if l.deps.SetNeedRestart != nil {
			l.deps.SetNeedRestart()
		}
		return err
	}
	if ib.Protocol == model.TUIC {
		inst, ok := tuic.InstanceFromInbound(ib)
		if !ok {
			return nil
		}
		err := tuic.GetManager().Ensure(inst)
		if l.deps.SetNeedRestart != nil {
			l.deps.SetNeedRestart()
		}
		return err
	}
	body, err := json.MarshalIndent(ib.GenXrayInboundConfig(), "", "  ")
	if err != nil {
		return err
	}
	return l.withAPI(func(api *xray.XrayAPI) error {
		return api.AddInbound(body)
	})
}

func (l *Local) DelInbound(_ context.Context, ib *model.Inbound) error {
	
	if ib.Protocol == model.AmneziaWG {
		amneziawgnet.GetManager().Remove(ib.Id)
		// The removed inbound may have been the only one backing Xray's
		// injectAmneziawgnetSocks relay inbound for this tag -- flag a
		// resync so the now-stale relay gets torn down promptly.
		if l.deps.SetNeedRestart != nil {
			l.deps.SetNeedRestart()
		}
		return nil
	}
	if ib.Protocol == model.TUIC {
		tuic.GetManager().Remove(ib.Id)
		if l.deps.SetNeedRestart != nil {
			l.deps.SetNeedRestart()
		}
		return nil
	}
	return l.withAPI(func(api *xray.XrayAPI) error {
		return api.DelInbound(ib.Tag)
	})
}

func (l *Local) UpdateInbound(ctx context.Context, oldIb, newIb *model.Inbound) error {
	
	if oldIb.Protocol == model.AmneziaWG || newIb.Protocol == model.AmneziaWG {
		return l.updateAmneziaWGInbound(ctx, oldIb, newIb)
	}
	if oldIb.Protocol == model.TUIC || newIb.Protocol == model.TUIC {
		return l.updateTuicInbound(ctx, oldIb, newIb)
	}
	_ = l.DelInbound(ctx, oldIb)
	if !newIb.Enable {
		return nil
	}
	return l.AddInbound(ctx, newIb)
}


// updateAmneziaWGInbound skips the
// Remove+Ensure sequence a plain Del+Add would force so that, on an
// AmneziaWG-to-AmneziaWG edit, Manager.Ensure's own fingerprint comparison
// can reconfigure the running embedded Device in place via IpcSet instead
// of always rebuilding it (see internal/amneziawgnet.Manager.ensureLocked --
// only an address or effective-MTU change forces a rebuild there, S4
// included, not a peer edit).
//
// Every exit path below only touches the embedded Device via
// amneziawgnet.GetManager() -- none of it rebuilds Xray's own config, which
// is what actually creates/removes injectAmneziawgnetSocks's relay inbound.
// A peer edit that changes whether this inbound has a qualifying peer at
// all (its first peer added, or its last one removed) must still get that
// relay created or torn down, so flag Xray for a resync unconditionally
// here rather than trying to enumerate which of the branches below need it.
func (l *Local) updateAmneziaWGInbound(ctx context.Context, oldIb, newIb *model.Inbound) error {
	if l.deps.SetNeedRestart != nil {
		l.deps.SetNeedRestart()
	}
	if oldIb.Protocol == model.AmneziaWG && newIb.Protocol != model.AmneziaWG {
		amneziawgnet.GetManager().Remove(oldIb.Id)
		if !newIb.Enable {
			return nil
		}
		return l.AddInbound(ctx, newIb)
	}
	if oldIb.Protocol != model.AmneziaWG {
		_ = l.DelInbound(ctx, oldIb)
	}
	if !newIb.Enable {
		amneziawgnet.GetManager().Remove(newIb.Id)
		return nil
	}
	inst, ok := amneziawg.InstanceFromInbound(newIb)
	if !ok {
		amneziawgnet.GetManager().Remove(newIb.Id)
		return nil
	}
	return amneziawgnet.GetManager().Ensure(amneziawgnet.Desired{
		Instance: inst,
		Options: amneziawgnet.DeviceOptions{
			HeaderProtectionKey:    inst.Obfuscation.HeaderProtectionKey,
			ContentPaddingAddition: inst.Obfuscation.ContentPaddingAddition,
			RekeyAfterTime:         inst.Obfuscation.RekeyAfterTime,
			RekeyTimeout:           inst.Obfuscation.RekeyTimeout,
			RejectAfterTime:        inst.Obfuscation.RejectAfterTime,
			KeepaliveTimeout:       inst.Obfuscation.KeepaliveTimeout,
			MaxHandshakeAttempts:   inst.Obfuscation.MaxHandshakeAttempts,
			RandomTrailers:         inst.Obfuscation.RandomTrailers,
			DisableCookies:         inst.Obfuscation.DisableCookies,
		},
	})
}

func (l *Local) updateTuicInbound(ctx context.Context, oldIb, newIb *model.Inbound) error {
	if oldIb.Protocol == model.TUIC && newIb.Protocol != model.TUIC {
		tuic.GetManager().Remove(oldIb.Id)
		if l.deps.SetNeedRestart != nil {
			l.deps.SetNeedRestart()
		}
		if !newIb.Enable {
			return nil
		}
		return l.AddInbound(ctx, newIb)
	}
	if oldIb.Protocol != model.TUIC {
		_ = l.DelInbound(ctx, oldIb)
		if l.deps.SetNeedRestart != nil {
			l.deps.SetNeedRestart()
		}
	}
	if oldIb.Protocol == model.TUIC && newIb.Protocol == model.TUIC && oldIb.Enable && newIb.Enable && oldIb.Tag != newIb.Tag && l.deps.SetNeedRestart != nil {
		l.deps.SetNeedRestart()
	}
	if !newIb.Enable {
		tuic.GetManager().Remove(newIb.Id)
		if oldIb.Enable && l.deps.SetNeedRestart != nil {
			l.deps.SetNeedRestart()
		}
		return nil
	}
	if !oldIb.Enable && newIb.Enable {
		if l.deps.SetNeedRestart != nil {
			l.deps.SetNeedRestart()
		}
	}
	inst, ok := tuic.InstanceFromInbound(newIb)
	if !ok {
		tuic.GetManager().Remove(newIb.Id)
		return nil
	}
	return tuic.GetManager().Ensure(inst)
}

func (l *Local) AddUser(_ context.Context, ib *model.Inbound, userMap map[string]any) error {
	if ib.Protocol == model.AmneziaWG || ib.Protocol == model.TUIC {
		return nil
	}
	return l.withAPI(func(api *xray.XrayAPI) error {
		return api.AddUser(string(ib.Protocol), ib.Tag, userMap)
	})
}

func (l *Local) RemoveUser(_ context.Context, ib *model.Inbound, email string) error {
	if ib.Protocol == model.AmneziaWG || ib.Protocol == model.TUIC {
		return nil
	}
	return l.withAPI(func(api *xray.XrayAPI) error {
		return api.RemoveUser(ib.Tag, email)
	})
}
