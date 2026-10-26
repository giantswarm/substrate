// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package csi

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/agent-substrate/substrate/internal/credbundle"
	"github.com/agent-substrate/substrate/internal/nodepath"
	"github.com/agent-substrate/substrate/internal/volume"
	v1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// DefaultClientCertPath is the path to the pod identity credential bundle.
	DefaultClientCertPath = "/run/podidentity.podcert.ate.dev/credential-bundle.pem"
	// DefaultCACertPath is the path to the servicedns trust bundle.
	DefaultCACertPath = "/run/servicedns.podcert.ate.dev/trust-bundle.pem"
)

type tlsPaths struct {
	clientCert string
	caCert     string
}

var defaultTLSPaths = tlsPaths{
	clientCert: DefaultClientCertPath,
	caCert:     DefaultCACertPath,
}

// Plugin implements volume.VolumePluginWorkerPlane using the CSI Client.
type Plugin struct {
	client           *Client
	stagingDirPrefix string

	// capsMu guards controllerCaps and nodeCaps. The maps are built by
	// InitControllerCapabilities and InitNodeCapabilities, and an RPC is
	// disabled once the driver returns Unimplemented for it.
	capsMu         sync.RWMutex
	controllerCaps map[csi.ControllerServiceCapability_RPC_Type]bool
	nodeCaps       map[csi.NodeServiceCapability_RPC_Type]bool
}

// Ensure Plugin implements volume.VolumePluginControlPlane and VolumePluginWorkerPlane
var _ volume.VolumePluginControlPlane = (*Plugin)(nil)
var _ volume.VolumePluginWorkerPlane = (*Plugin)(nil)

// NewPlugin creates a new Plugin adapter. Call InitControllerCapabilities or
// InitNodeCapabilities before use; until then, no optional RPC is reported as supported.
func NewPlugin(client *Client) *Plugin {
	return &Plugin{
		client:           client,
		stagingDirPrefix: filepath.Join(nodepath.BasePath, "staging"),
	}
}

// SupportsControllerCapability reports whether the driver supports the given controller RPC.
func (p *Plugin) SupportsControllerCapability(rpc csi.ControllerServiceCapability_RPC_Type) bool {
	p.capsMu.RLock()
	defer p.capsMu.RUnlock()
	return p.controllerCaps[rpc]
}

// SupportsNodeCapability reports whether the driver supports the given node RPC.
func (p *Plugin) SupportsNodeCapability(rpc csi.NodeServiceCapability_RPC_Type) bool {
	p.capsMu.RLock()
	defer p.capsMu.RUnlock()
	return p.nodeCaps[rpc]
}

// disableControllerCap records that the driver does not implement the given controller RPC.
func (p *Plugin) disableControllerCap(rpc csi.ControllerServiceCapability_RPC_Type) {
	p.capsMu.Lock()
	defer p.capsMu.Unlock()
	if p.controllerCaps == nil {
		p.controllerCaps = make(map[csi.ControllerServiceCapability_RPC_Type]bool)
	}
	p.controllerCaps[rpc] = false
}

// disableNodeCap records that the driver does not implement the given node RPC.
func (p *Plugin) disableNodeCap(rpc csi.NodeServiceCapability_RPC_Type) {
	p.capsMu.Lock()
	defer p.capsMu.Unlock()
	if p.nodeCaps == nil {
		p.nodeCaps = make(map[csi.NodeServiceCapability_RPC_Type]bool)
	}
	p.nodeCaps[rpc] = false
}

// InitControllerCapabilities queries and caches ControllerGetCapabilities from the driver.
// If the query fails or reports no capabilities, every controller RPC is assumed supported.
func (p *Plugin) InitControllerCapabilities(ctx context.Context) {
	caps := make(map[csi.ControllerServiceCapability_RPC_Type]bool)
	resp, err := p.client.ControllerGetCapabilities(ctx, &csi.ControllerGetCapabilitiesRequest{})
	for _, c := range resp.GetCapabilities() {
		if rpc := c.GetRpc(); rpc != nil {
			caps[rpc.GetType()] = true
		}
	}
	if err != nil || len(caps) == 0 {
		slog.WarnContext(ctx, "CSI ControllerGetCapabilities failed or reported no capabilities; assuming all controller RPCs are supported", slog.Any("error", err))
		for v := range csi.ControllerServiceCapability_RPC_Type_name {
			if rpc := csi.ControllerServiceCapability_RPC_Type(v); rpc != csi.ControllerServiceCapability_RPC_UNKNOWN {
				caps[rpc] = true
			}
		}
	}
	p.capsMu.Lock()
	defer p.capsMu.Unlock()
	p.controllerCaps = caps
}

// InitNodeCapabilities queries and caches NodeGetCapabilities from the driver.
// If the query fails or reports no capabilities, every node RPC is assumed supported.
func (p *Plugin) InitNodeCapabilities(ctx context.Context) {
	caps := make(map[csi.NodeServiceCapability_RPC_Type]bool)
	resp, err := p.client.NodeGetCapabilities(ctx, &csi.NodeGetCapabilitiesRequest{})
	for _, c := range resp.GetCapabilities() {
		if rpc := c.GetRpc(); rpc != nil {
			caps[rpc.GetType()] = true
		}
	}
	if err != nil || len(caps) == 0 {
		slog.WarnContext(ctx, "CSI NodeGetCapabilities failed or reported no capabilities; assuming all node RPCs are supported", slog.Any("error", err))
		for v := range csi.NodeServiceCapability_RPC_Type_name {
			if rpc := csi.NodeServiceCapability_RPC_Type(v); rpc != csi.NodeServiceCapability_RPC_UNKNOWN {
				caps[rpc] = true
			}
		}
	}
	p.capsMu.Lock()
	defer p.capsMu.Unlock()
	p.nodeCaps = caps
}

// DriverName returns the driver name obtained from the CSI plugin.
func (p *Plugin) DriverName(ctx context.Context) (string, error) {
	resp, err := p.client.GetPluginInfo(ctx, &csi.GetPluginInfoRequest{})
	if err != nil {
		return "", err
	}
	return resp.GetName(), nil
}

// CreateVolume maps to CSI Controller CreateVolume.
func (p *Plugin) CreateVolume(ctx context.Context, req volume.CreateVolumeRequest) (volume.CreateVolumeResponse, error) {
	qty, err := resource.ParseQuantity(req.Capacity)
	if err != nil {
		return volume.CreateVolumeResponse{}, fmt.Errorf("failed to parse capacity %q: %w", req.Capacity, err)
	}
	capBytes := qty.Value()

	csiReq := &csi.CreateVolumeRequest{
		Name: req.Name,
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: capBytes,
		},
		VolumeCapabilities: getStandardCapabilities(),
		Parameters:         req.Parameters,
	}

	resp, err := p.client.CreateVolume(ctx, csiReq)
	if err != nil {
		return volume.CreateVolumeResponse{}, fmt.Errorf("CSI CreateVolume failed: %w", err)
	}

	if resp.GetVolume() == nil {
		return volume.CreateVolumeResponse{}, fmt.Errorf("CSI CreateVolume response returned nil volume")
	}

	return volume.CreateVolumeResponse{
		VolumeID:      resp.GetVolume().GetVolumeId(),
		VolumeContext: resp.GetVolume().GetVolumeContext(),
	}, nil
}

// DeleteVolume maps to CSI Controller DeleteVolume.
func (p *Plugin) DeleteVolume(ctx context.Context, volumeID string) error {
	req := &csi.DeleteVolumeRequest{
		VolumeId: volumeID,
	}

	_, err := p.client.DeleteVolume(ctx, req)
	if err != nil {
		return fmt.Errorf("CSI DeleteVolume failed: %w", err)
	}
	return nil
}

// AttachVolume maps to CSI Controller ControllerPublishVolume.
func (p *Plugin) AttachVolume(ctx context.Context, req volume.AttachVolumeRequest) (volume.AttachVolumeResponse, error) {
	if !p.SupportsControllerCapability(csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME) {
		slog.DebugContext(ctx, "Driver does not support ControllerPublishVolume; skipping attach", slog.String("volume_id", req.VolumeID), slog.String("node", req.Node))
		return volume.AttachVolumeResponse{}, nil
	}

	csiReq := &csi.ControllerPublishVolumeRequest{
		VolumeId:         req.VolumeID,
		NodeId:           req.Node,
		VolumeCapability: mountCapability(req.Mode),
		Readonly:         false,
	}

	resp, err := p.client.ControllerPublishVolume(ctx, csiReq)
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			slog.WarnContext(ctx, "CSI ControllerPublishVolume is unimplemented by driver; skipping attach", slog.String("volume_id", req.VolumeID), slog.String("node", req.Node))
			p.disableControllerCap(csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME)
			return volume.AttachVolumeResponse{}, nil
		}
		return volume.AttachVolumeResponse{}, fmt.Errorf("CSI ControllerPublishVolume failed: %w", err)
	}

	return volume.AttachVolumeResponse{PublishContext: resp.GetPublishContext()}, nil
}

// DetachVolume maps to CSI Controller ControllerUnpublishVolume.
func (p *Plugin) DetachVolume(ctx context.Context, volumeID string, node string) error {
	if !p.SupportsControllerCapability(csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME) {
		slog.DebugContext(ctx, "Driver does not support ControllerPublishVolume; skipping detach", slog.String("volume_id", volumeID), slog.String("node", node))
		return nil
	}

	req := &csi.ControllerUnpublishVolumeRequest{
		VolumeId: volumeID,
		NodeId:   node,
	}

	_, err := p.client.ControllerUnpublishVolume(ctx, req)
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			slog.WarnContext(ctx, "CSI ControllerUnpublishVolume is unimplemented by driver; skipping detach", slog.String("volume_id", volumeID), slog.String("node", node))
			p.disableControllerCap(csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME)
			return nil
		}
		return fmt.Errorf("CSI ControllerUnpublishVolume failed: %w", err)
	}
	return nil
}

// MountVolume maps to CSI Node NodePublishVolume.
// It also handles NodeStageVolume staging if required by the driver.
// Like the kubelet, it requests the volume's own access mode and expresses a
// read-only mount as the publish's readonly flag: a driver may support only
// the volume's mode (the AWS EFS driver refuses MULTI_NODE_READER_ONLY for a
// read-write-many volume).
func (p *Plugin) MountVolume(ctx context.Context, req volume.MountVolumeRequest) error {
	if p.stagesShared(req.Mode) {
		defer lockStaging(p.volumeStagingDir(req.VolumeID))()
	}
	stagingPath, err := p.stageVolume(ctx, req)
	if err != nil {
		return err
	}

	csiReq := &csi.NodePublishVolumeRequest{
		VolumeId:          req.VolumeID,
		StagingTargetPath: stagingPath,
		TargetPath:        req.TargetPath,
		VolumeCapability:  mountCapability(req.Mode),
		Readonly:          req.ReadOnly || req.Mode == volume.ReadOnlyMany,
		VolumeContext:     req.VolumeContext,
		PublishContext:    req.PublishContext,
	}

	if _, err := p.client.NodePublishVolume(ctx, csiReq); err != nil {
		err = fmt.Errorf("CSI NodePublishVolume failed: %w", err)
		if stagingPath != "" && req.Mode.MultiNode() {
			if releaseErr := p.releaseSharedStaging(ctx, req.VolumeID, req.TargetPath); releaseErr != nil {
				slog.WarnContext(ctx, "failed to release the staging of a volume whose publish failed", slog.String("volume_id", req.VolumeID), slog.Any("error", releaseErr))
			}
		}
		return err
	}
	return nil
}

// stageVolume calls NodeStageVolume if the driver supports it. It returns the staging
// path, or an empty string if the volume was not staged. A volume of a
// multi-node mode is staged at one path per node, which every target shares
// (the CSI spec allows one staging_target_path per volume): staging it again
// is the driver's idempotent no-op, and the target is recorded so that the
// last target's unmount unstages it.
func (p *Plugin) stageVolume(ctx context.Context, req volume.MountVolumeRequest) (string, error) {
	if !p.SupportsNodeCapability(csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME) {
		return "", nil
	}

	stagingPath := p.stagingPath(req.VolumeID, req.Mode)
	if err := os.MkdirAll(stagingPath, 0750); err != nil {
		return "", fmt.Errorf("failed to create staging directory %q: %w", stagingPath, err)
	}

	csiReq := &csi.NodeStageVolumeRequest{
		VolumeId:          req.VolumeID,
		StagingTargetPath: stagingPath,
		VolumeCapability:  mountCapability(req.Mode),
		VolumeContext:     req.VolumeContext,
		PublishContext:    req.PublishContext,
	}

	_, err := p.client.NodeStageVolume(ctx, csiReq)
	if status.Code(err) == codes.Unimplemented {
		slog.WarnContext(ctx, "CSI NodeStageVolume is unimplemented by driver; skipping staging", slog.String("volume_id", req.VolumeID))
		p.disableNodeCap(csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME)
		removeStagingDir(ctx, stagingPath)
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("CSI NodeStageVolume failed: %w", err)
	}
	if req.Mode.MultiNode() {
		if err := p.recordTarget(req.VolumeID, req.TargetPath); err != nil {
			if releaseErr := p.releaseSharedStaging(ctx, req.VolumeID, req.TargetPath); releaseErr != nil {
				slog.WarnContext(ctx, "failed to release the staging of a volume whose target was not recorded", slog.String("volume_id", req.VolumeID), slog.Any("error", releaseErr))
			}
			return "", err
		}
	}
	return stagingPath, nil
}

// UnmountVolume maps to CSI Node NodeUnpublishVolume.
// It also handles NodeUnstageVolume if staging was used.
func (p *Plugin) UnmountVolume(ctx context.Context, volumeID string, targetPath string, mode volume.AccessMode) error {
	if p.stagesShared(mode) {
		defer lockStaging(p.volumeStagingDir(volumeID))()
	}
	req := &csi.NodeUnpublishVolumeRequest{
		VolumeId:   volumeID,
		TargetPath: targetPath,
	}

	if _, err := p.client.NodeUnpublishVolume(ctx, req); err != nil {
		return fmt.Errorf("CSI NodeUnpublishVolume failed: %w", err)
	}

	return p.unstageVolume(ctx, volumeID, targetPath, mode)
}

// unstageVolume calls NodeUnstageVolume and removes the staging directory if the
// driver supports staging. A volume of a multi-node mode stays staged while
// another target on the node still has it.
func (p *Plugin) unstageVolume(ctx context.Context, volumeID, targetPath string, mode volume.AccessMode) error {
	if !p.SupportsNodeCapability(csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME) {
		return nil
	}
	if mode.MultiNode() {
		return p.releaseSharedStaging(ctx, volumeID, targetPath)
	}
	return p.unstageAt(ctx, volumeID, p.stagingPath(volumeID, mode))
}

// unstageAt calls NodeUnstageVolume for one staging path and removes its
// directory.
func (p *Plugin) unstageAt(ctx context.Context, volumeID, stagingPath string) error {
	req := &csi.NodeUnstageVolumeRequest{
		VolumeId:          volumeID,
		StagingTargetPath: stagingPath,
	}

	_, err := p.client.NodeUnstageVolume(ctx, req)
	if status.Code(err) == codes.Unimplemented {
		slog.WarnContext(ctx, "CSI NodeUnstageVolume is unimplemented by driver; skipping unstaging", slog.String("volume_id", volumeID))
		p.disableNodeCap(csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME)
	} else if err != nil {
		return fmt.Errorf("CSI NodeUnstageVolume failed: %w", err)
	}

	removeStagingDir(ctx, stagingPath)
	return nil
}

// The layout of a multi-node volume's staging directory on the node
// (volumeStagingDir): the one staging path every target shares, and a file
// per target that has the volume published, named by targetKey. A release
// before this layout staged each target at <volumeStagingDir>/<targetKey>;
// such a staging is unstaged where it is.
const (
	sharedStagingDir = "staged"
	stagedTargetsDir = "targets"
)

// stagesShared reports whether a mount of the mode goes through the shared
// staging of a multi-node volume.
func (p *Plugin) stagesShared(mode volume.AccessMode) bool {
	return mode.MultiNode() && p.SupportsNodeCapability(csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME)
}

// volumeStagingDir is the directory of a volume's staging on the node.
func (p *Plugin) volumeStagingDir(volumeID string) string {
	return filepath.Join(p.stagingDirPrefix, volumeID)
}

// stagingPath is where a volume is staged on the node: once per node, a
// multi-node volume's shared by every target.
func (p *Plugin) stagingPath(volumeID string, mode volume.AccessMode) string {
	if !mode.MultiNode() {
		return p.volumeStagingDir(volumeID)
	}
	return filepath.Join(p.volumeStagingDir(volumeID), sharedStagingDir)
}

// targetKey names a target path within a volume's staging directory.
func targetKey(targetPath string) string {
	sum := sha256.Sum256([]byte(targetPath))
	return hex.EncodeToString(sum[:8])
}

// recordTarget records that a target has a multi-node volume published.
func (p *Plugin) recordTarget(volumeID, targetPath string) error {
	dir := filepath.Join(p.volumeStagingDir(volumeID), stagedTargetsDir)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create the staged targets directory %q: %w", dir, err)
	}
	record := filepath.Join(dir, targetKey(targetPath))
	if err := os.WriteFile(record, nil, 0640); err != nil {
		return fmt.Errorf("failed to record the staged target %q: %w", record, err)
	}
	return nil
}

// releaseSharedStaging drops a target's hold on a multi-node volume's
// staging and unstages the volume once no target holds it. A target a
// release before the shared layout staged on its own is unstaged at its own
// path.
func (p *Plugin) releaseSharedStaging(ctx context.Context, volumeID, targetPath string) error {
	dir := p.volumeStagingDir(volumeID)
	key := targetKey(targetPath)
	if fi, err := os.Stat(filepath.Join(dir, key)); err == nil && fi.IsDir() {
		if err := p.unstageAt(ctx, volumeID, filepath.Join(dir, key)); err != nil {
			return err
		}
		_ = os.Remove(dir)
		return nil
	}

	targets := filepath.Join(dir, stagedTargetsDir)
	if err := os.Remove(filepath.Join(targets, key)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove the staged target record of %q: %w", targetPath, err)
	}
	remaining, err := os.ReadDir(targets)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read the staged targets of volume %q: %w", volumeID, err)
	}
	if len(remaining) > 0 {
		return nil
	}
	if err := p.unstageAt(ctx, volumeID, filepath.Join(dir, sharedStagingDir)); err != nil {
		return err
	}
	_ = os.Remove(targets)
	_ = os.Remove(dir)
	return nil
}

// stagingLocks serializes the staging of one multi-node volume across every
// Plugin of the process, by its staging directory: a mount must not stage
// while the last other target's unmount unstages.
var stagingLocks sync.Map

// lockStaging locks a volume's staging and returns the unlock.
func lockStaging(dir string) func() {
	mu, _ := stagingLocks.LoadOrStore(dir, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	return mu.(*sync.Mutex).Unlock
}

// mountCapability is the mount capability of an access mode.
func mountCapability(mode volume.AccessMode) *csi.VolumeCapability {
	capability := getStandardCapabilities()[0]
	switch mode {
	case volume.ReadOnlyMany:
		capability.AccessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case volume.ReadWriteMany:
		capability.AccessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	}
	return capability
}

// removeStagingDir removes an empty staging directory, logging any failure.
func removeStagingDir(ctx context.Context, stagingPath string) {
	if err := os.Remove(stagingPath); err != nil && !os.IsNotExist(err) {
		slog.WarnContext(ctx, "failed to remove staging directory", slog.String("path", stagingPath), slog.Any("error", err))
	}
}

// Helper to provide standard capabilities for general volume operations.
// TODO: Support and expose different volume access modes (e.g. ReadWriteMany, ReadOnlyMany)
// instead of hardcoding SingleNodeWriter.
func getStandardCapabilities() []*csi.VolumeCapability {
	return []*csi.VolumeCapability{
		{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
	}
}

// CSIDriverConfigGetter provides access to retrieve a CSIDriverConfig by name.
// Both listersv1alpha1.CSIDriverConfigLister and direct client getters implement this interface.
type CSIDriverConfigGetter interface {
	Get(name string) (*v1alpha1.CSIDriverConfig, error)
}

// NewCSIPlugin establishes a CSI client and returns a verified Plugin instance.
func NewCSIPlugin(ctx context.Context, getter CSIDriverConfigGetter, driverName string, isController bool) (*Plugin, error) {
	return newCSIPlugin(ctx, getter, driverName, isController, defaultTLSPaths)
}

func newCSIPlugin(ctx context.Context, getter CSIDriverConfigGetter, driverName string, isController bool, paths tlsPaths) (*Plugin, error) {
	if getter == nil {
		return nil, fmt.Errorf("missing csiDriverConfigGetter")
	}

	cfg, err := getter.Get(driverName)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve CSIDriverConfig for %q: %w", driverName, err)
	}

	var endpoint string
	switch {
	case isController:
		endpoint = cfg.Spec.ControllerEndpoint
	case cfg.Spec.NodeSocketOverride != "":
		endpoint = cfg.Spec.NodeSocketOverride
		slog.InfoContext(ctx, "Found CSIDriverConfig with NodeSocketOverride", slog.String("driver", driverName), slog.String("endpoint", endpoint))
	default:
		endpoint = "unix://" + kubeletPluginSocketPath(driverName)
	}

	var tlsCfg *tls.Config
	if isController {
		var err error
		tlsCfg, err = resolveTLSConfig(cfg, paths)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve TLS config for %q: %w", driverName, err)
		}
	}

	csiClient, err := NewCSIClient(endpoint, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize CSI client from endpoint %q: %w", endpoint, err)
	}
	csiPlugin := NewPlugin(csiClient)

	// Verify CSI plugin reported name matches requested name.
	reportedName, err := csiPlugin.DriverName(ctx)
	if err != nil {
		csiClient.Close()
		return nil, fmt.Errorf("failed to get driver name from plugin %q: %w", driverName, err)
	}
	if reportedName != driverName {
		csiClient.Close()
		return nil, fmt.Errorf("reported driver name %q does not match requested name %q", reportedName, driverName)
	}

	// Capability discovery is best-effort; if it fails or reports nothing, every RPC is assumed supported.
	if isController {
		csiPlugin.InitControllerCapabilities(ctx)
	} else {
		csiPlugin.InitNodeCapabilities(ctx)
	}

	return csiPlugin, nil
}

func resolveTLSConfig(cfg *v1alpha1.CSIDriverConfig, paths tlsPaths) (*tls.Config, error) {
	if cfg == nil || cfg.Spec.TLS == nil || !cfg.Spec.TLS.Enabled {
		return nil, nil
	}

	tlsCfg := cfg.Spec.TLS

	if !tlsCfg.UsePodIdentity {
		// TODO: Support manual certificates loaded from Secrets specified in the config.
		return nil, fmt.Errorf("only pod identity TLS is supported in this configuration")
	}

	caCache := newCAPoolCache(paths.caCert)

	// Verify CA pool exists, is readable, and populate the initial cache.
	if _, err := caCache.getCertPool(); err != nil {
		return nil, fmt.Errorf("failed to load CA cert pool from %q: %w", paths.caCert, err)
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: tlsCfg.ServerName,
		// NextProtos configures ALPN h2 for gRPC over TLS.
		NextProtos: []string{"h2"},
		// Load Client Certificate dynamically.
		// In Substrate's Pod Identity model, certificates are projected into the pod
		// as files by the kubelet (via Substrate's podcertcontroller).
		// Kubelet handles the rotation of these files on disk.
		// credbundle.ClientLoader monitors these files and automatically reloads
		// them when they change, ensuring rotation is picked up on subsequent handshakes.
		GetClientCertificate: credbundle.ClientLoader(paths.clientCert),
		// Dynamic CA Reloading:
		// Standard tls.Config.RootCAs is a static cert pool evaluated at construction time.
		// To automatically pick up CA trust bundle rotations on disk without restarting the process,
		// we set InsecureSkipVerify=true and verify the server certificate chain dynamically
		// against the CA bundle in VerifyConnection.
		// caCache avoids re-reading and re-parsing the CA bundle from disk on every handshake
		// unless the file has changed.
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("server did not present certificates")
			}

			roots, err := caCache.getCertPool()
			if err != nil {
				return fmt.Errorf("failed to load CA cert pool from %q: %w", paths.caCert, err)
			}

			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}

			leaf := state.PeerCertificates[0]
			opts := x509.VerifyOptions{
				DNSName:       tlsCfg.ServerName,
				Roots:         roots,
				Intermediates: intermediates,
				KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			}
			if _, err := leaf.Verify(opts); err != nil {
				return fmt.Errorf("failed to verify server certificate against CA in %q: %w", paths.caCert, err)
			}
			return nil
		},
	}, nil
}

// caPoolCache holds the parsed *x509.CertPool and file stat so unchanged CA trust bundles
// are not re-read from disk on every TLS handshake.
type caPoolCache struct {
	path string

	mu   sync.Mutex
	fi   os.FileInfo
	pool *x509.CertPool
}

func newCAPoolCache(path string) *caPoolCache {
	return &caPoolCache{path: path}
}

// isFileUnchanged reports whether newFi is the same file as the stat the cached
// pool was parsed from.
func (c *caPoolCache) isFileUnchanged(newFi os.FileInfo) bool {
	if c.fi == nil || newFi == nil {
		return false
	}
	return os.SameFile(c.fi, newFi) && c.fi.ModTime().Equal(newFi.ModTime()) && c.fi.Size() == newFi.Size()
}

// getCertPool returns the parsed CA cert pool, re-reading the file only when it has changed
// on disk (identity, modification time, or size).
func (c *caPoolCache) getCertPool() (*x509.CertPool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fi, err := os.Stat(c.path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat CA cert file %q: %w", c.path, err)
	}

	if c.pool != nil && c.isFileUnchanged(fi) {
		return c.pool, nil
	}

	pool, err := parseCertPool(c.path)
	if err != nil {
		return nil, err
	}

	c.fi, c.pool = fi, pool
	return c.pool, nil
}

func parseCertPool(path string) (*x509.CertPool, error) {
	certBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read cert file %q: %w", path, err)
	}

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(certBytes); !ok {
		return nil, fmt.Errorf("failed to parse any certificates from %q", path)
	}
	return pool, nil
}

// kubeletPluginSocketPath is the CSI driver socket in the kubelet plugins directory.
func kubeletPluginSocketPath(driverName string) string {
	return filepath.Join("/var/lib/kubelet/plugins", driverName, "csi.sock")
}
