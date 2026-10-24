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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/agent-substrate/substrate/internal/volume"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mockCSIDriver struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedControllerServer
	csi.UnimplementedNodeServer

	createVolumeFunc              func(context.Context, *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error)
	deleteVolumeFunc              func(context.Context, *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error)
	controllerPublishVolumeFunc   func(context.Context, *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error)
	controllerUnpublishVolumeFunc func(context.Context, *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error)
	nodeStageVolumeFunc           func(context.Context, *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error)
	nodeUnstageVolumeFunc         func(context.Context, *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error)
	nodePublishVolumeFunc         func(context.Context, *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error)
	nodeUnpublishVolumeFunc       func(context.Context, *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error)

	controllerGetCapabilitiesFunc func(context.Context, *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error)
	nodeGetCapabilitiesFunc       func(context.Context, *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error)
	getPluginCapabilitiesFunc     func(context.Context, *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error)
	probeFunc                     func(context.Context, *csi.ProbeRequest) (*csi.ProbeResponse, error)
}

func (m *mockCSIDriver) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          "mock-driver",
		VendorVersion: "v1.0.0",
	}, nil
}

func (m *mockCSIDriver) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	if m.getPluginCapabilitiesFunc != nil {
		return m.getPluginCapabilitiesFunc(ctx, req)
	}
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}, nil
}

func (m *mockCSIDriver) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	if m.probeFunc != nil {
		return m.probeFunc(ctx, req)
	}
	return &csi.ProbeResponse{}, nil
}

func (m *mockCSIDriver) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if m.createVolumeFunc != nil {
		return m.createVolumeFunc(ctx, req)
	}
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      req.GetName(),
			CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
		},
	}, nil
}

func (m *mockCSIDriver) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if m.deleteVolumeFunc != nil {
		return m.deleteVolumeFunc(ctx, req)
	}
	return &csi.DeleteVolumeResponse{}, nil
}

func (m *mockCSIDriver) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if m.controllerPublishVolumeFunc != nil {
		return m.controllerPublishVolumeFunc(ctx, req)
	}
	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (m *mockCSIDriver) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if m.controllerUnpublishVolumeFunc != nil {
		return m.controllerUnpublishVolumeFunc(ctx, req)
	}
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (m *mockCSIDriver) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	if m.controllerGetCapabilitiesFunc != nil {
		return m.controllerGetCapabilitiesFunc(ctx, req)
	}
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (m *mockCSIDriver) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	if m.nodeGetCapabilitiesFunc != nil {
		return m.nodeGetCapabilitiesFunc(ctx, req)
	}
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (m *mockCSIDriver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if m.nodeStageVolumeFunc != nil {
		return m.nodeStageVolumeFunc(ctx, req)
	}
	return &csi.NodeStageVolumeResponse{}, nil
}

func (m *mockCSIDriver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if m.nodeUnstageVolumeFunc != nil {
		return m.nodeUnstageVolumeFunc(ctx, req)
	}
	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (m *mockCSIDriver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if m.nodePublishVolumeFunc != nil {
		return m.nodePublishVolumeFunc(ctx, req)
	}
	return &csi.NodePublishVolumeResponse{}, nil
}

func (m *mockCSIDriver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if m.nodeUnpublishVolumeFunc != nil {
		return m.nodeUnpublishVolumeFunc(ctx, req)
	}
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func startMockCSIDriver(t *testing.T, driver *mockCSIDriver) (string, func()) {
	tmpDir, err := os.MkdirTemp("", "csi-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	socketPath := filepath.Join(tmpDir, "csi.sock")
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to listen on socket %q: %v", socketPath, err)
	}

	s := grpc.NewServer()
	csi.RegisterIdentityServer(s, driver)
	csi.RegisterControllerServer(s, driver)
	csi.RegisterNodeServer(s, driver)

	go func() {
		if err := s.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			t.Errorf("grpc server failed: %v", err)
		}
	}()

	cleanup := func() {
		s.GracefulStop()
		lis.Close()
		os.RemoveAll(tmpDir)
	}

	return "unix://" + socketPath, cleanup
}

func TestPlugin_CreateVolume(t *testing.T) {
	driver := &mockCSIDriver{}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	plugin := NewPlugin(client)

	ctx := context.Background()
	resp, err := plugin.CreateVolume(ctx, volume.CreateVolumeRequest{
		Name:     "test-vol",
		Capacity: "1Gi",
	})
	if err != nil {
		t.Fatalf("CreateVolume failed: %v", err)
	}

	if resp.VolumeID != "test-vol" {
		t.Errorf("expected volume ID %q, got %q", "test-vol", resp.VolumeID)
	}
}

func TestPlugin_DeleteVolume(t *testing.T) {
	driver := &mockCSIDriver{}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	plugin := NewPlugin(client)

	ctx := context.Background()
	err = plugin.DeleteVolume(ctx, "test-vol")
	if err != nil {
		t.Fatalf("DeleteVolume failed: %v", err)
	}
}

func TestPlugin_AttachVolume(t *testing.T) {
	driver := &mockCSIDriver{}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	plugin := NewPlugin(client)

	ctx := context.Background()
	plugin.InitControllerCapabilities(ctx)
	resp, err := plugin.AttachVolume(ctx, volume.AttachVolumeRequest{VolumeID: "test-vol", Node: "node-1"})
	if err != nil {
		t.Fatalf("AttachVolume failed: %v", err)
	}
	if len(resp.PublishContext) != 0 {
		t.Errorf("expected no publish context from a driver that returns none, got %v", resp.PublishContext)
	}

	driver.controllerPublishVolumeFunc = func(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
		return &csi.ControllerPublishVolumeResponse{
			PublishContext: map[string]string{"devicePath": "/dev/xvdba"},
		}, nil
	}
	resp, err = plugin.AttachVolume(ctx, volume.AttachVolumeRequest{VolumeID: "test-vol", Node: "node-1"})
	if err != nil {
		t.Fatalf("AttachVolume failed: %v", err)
	}
	wantPublishCtx := map[string]string{"devicePath": "/dev/xvdba"}
	if diff := cmp.Diff(wantPublishCtx, resp.PublishContext); diff != "" {
		t.Errorf("publish context mismatch (-want +got):\n%s", diff)
	}

	// Test Unimplemented warning bypass
	driver.controllerPublishVolumeFunc = func(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
		return nil, status.Error(codes.Unimplemented, "unimplemented")
	}
	resp, err = plugin.AttachVolume(ctx, volume.AttachVolumeRequest{VolumeID: "test-vol", Node: "node-1"})
	if err != nil {
		t.Errorf("AttachVolume should have ignored Unimplemented error, got: %v", err)
	}
	if len(resp.PublishContext) != 0 {
		t.Errorf("expected no publish context from an unimplemented attach, got %v", resp.PublishContext)
	}
}

func TestPlugin_DetachVolume(t *testing.T) {
	driver := &mockCSIDriver{}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	plugin := NewPlugin(client)

	ctx := context.Background()
	plugin.InitControllerCapabilities(ctx)
	err = plugin.DetachVolume(ctx, "test-vol", "node-1")
	if err != nil {
		t.Fatalf("DetachVolume failed: %v", err)
	}

	// Test Unimplemented warning bypass
	driver.controllerUnpublishVolumeFunc = func(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
		return nil, status.Error(codes.Unimplemented, "unimplemented")
	}
	err = plugin.DetachVolume(ctx, "test-vol", "node-1")
	if err != nil {
		t.Errorf("DetachVolume should have ignored Unimplemented error, got: %v", err)
	}
}

func TestPlugin_MountVolume(t *testing.T) {
	driver := &mockCSIDriver{}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	plugin := NewPlugin(client)
	tmpDir, err := os.MkdirTemp("", "csi-mount-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	plugin.stagingDirPrefix = filepath.Join(tmpDir, "staging")
	targetPath := filepath.Join(tmpDir, "target")

	ctx := context.Background()
	plugin.InitNodeCapabilities(ctx)
	err = plugin.MountVolume(ctx, volume.MountVolumeRequest{
		VolumeID:       "test-vol",
		TargetPath:     targetPath,
		PublishContext: map[string]string{"devicePath": "/dev/xvdba"},
	})
	if err != nil {
		t.Fatalf("MountVolume failed: %v", err)
	}

	// Verify staging directory was created
	stagingPath := filepath.Join(plugin.stagingDirPrefix, "test-vol")
	if _, err := os.Stat(stagingPath); os.IsNotExist(err) {
		t.Errorf("staging directory %q was not created", stagingPath)
	}

	// Test NodeStageVolume Unimplemented bypass
	driver.nodeStageVolumeFunc = func(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
		return nil, status.Error(codes.Unimplemented, "unimplemented")
	}
	// Reset dir
	os.RemoveAll(tmpDir)
	os.MkdirAll(plugin.stagingDirPrefix, 0750)

	err = plugin.MountVolume(ctx, volume.MountVolumeRequest{VolumeID: "test-vol-2", TargetPath: targetPath})
	if err != nil {
		t.Errorf("MountVolume should have succeeded when NodeStageVolume is unimplemented, got: %v", err)
	}
}

func TestPlugin_UnmountVolume(t *testing.T) {
	driver := &mockCSIDriver{}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	plugin := NewPlugin(client)
	tmpDir, err := os.MkdirTemp("", "csi-unmount-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	plugin.stagingDirPrefix = filepath.Join(tmpDir, "staging")
	targetPath := filepath.Join(tmpDir, "target")

	// Pre-create staging directory to test cleanup
	stagingPath := filepath.Join(plugin.stagingDirPrefix, "test-vol")
	if err := os.MkdirAll(stagingPath, 0750); err != nil {
		t.Fatalf("failed to create staging path: %v", err)
	}

	ctx := context.Background()
	plugin.InitNodeCapabilities(ctx)
	err = plugin.UnmountVolume(ctx, "test-vol", targetPath, volume.ReadWriteOnce)
	if err != nil {
		t.Fatalf("UnmountVolume failed: %v", err)
	}

	// Verify staging directory was deleted
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Errorf("staging directory %q was not deleted", stagingPath)
	}

	// Test NodeUnstageVolume Unimplemented bypass
	driver.nodeUnstageVolumeFunc = func(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
		return nil, status.Error(codes.Unimplemented, "unimplemented")
	}
	// Re-create staging dir
	if err := os.MkdirAll(stagingPath, 0750); err != nil {
		t.Fatalf("failed to create staging path: %v", err)
	}
	err = plugin.UnmountVolume(ctx, "test-vol", targetPath, volume.ReadWriteOnce)
	if err != nil {
		t.Errorf("UnmountVolume should have succeeded when NodeUnstageVolume is unimplemented, got: %v", err)
	}
}

func TestClient_Identity(t *testing.T) {
	driver := &mockCSIDriver{}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Test GetPluginInfo
	info, err := client.GetPluginInfo(ctx, &csi.GetPluginInfoRequest{})
	if err != nil {
		t.Fatalf("GetPluginInfo failed: %v", err)
	}
	if info.GetName() != "mock-driver" {
		t.Errorf("expected plugin name %q, got %q", "mock-driver", info.GetName())
	}

	// Test GetPluginCapabilities
	caps, err := client.GetPluginCapabilities(ctx, &csi.GetPluginCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("GetPluginCapabilities failed: %v", err)
	}
	if len(caps.GetCapabilities()) == 0 {
		t.Errorf("expected capabilities, got none")
	}

	// Test Probe
	_, err = client.Probe(ctx, &csi.ProbeRequest{})
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
}

func TestPlugin_Capabilities_SkipAttachDetachWhenUnsupported(t *testing.T) {
	var publishCalled, unpublishCalled int
	driver := &mockCSIDriver{
		controllerGetCapabilitiesFunc: func(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
			// Return capabilities WITHOUT PUBLISH_UNPUBLISH_VOLUME (like hostpath driver)
			return &csi.ControllerGetCapabilitiesResponse{
				Capabilities: []*csi.ControllerServiceCapability{
					{
						Type: &csi.ControllerServiceCapability_Rpc{
							Rpc: &csi.ControllerServiceCapability_RPC{
								Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
							},
						},
					},
				},
			}, nil
		},
		controllerPublishVolumeFunc: func(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
			publishCalled++
			return &csi.ControllerPublishVolumeResponse{}, nil
		},
		controllerUnpublishVolumeFunc: func(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
			unpublishCalled++
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		},
	}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	plugin := NewPlugin(client)
	ctx := context.Background()

	plugin.InitControllerCapabilities(ctx)

	if plugin.SupportsControllerCapability(csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME) {
		t.Errorf("expected PUBLISH_UNPUBLISH_VOLUME to be false")
	}

	// AttachVolume should skip calling ControllerPublishVolume entirely
	if _, err := plugin.AttachVolume(ctx, volume.AttachVolumeRequest{VolumeID: "test-vol", Node: "node-1"}); err != nil {
		t.Errorf("AttachVolume failed: %v", err)
	}
	if publishCalled != 0 {
		t.Errorf("expected 0 calls to ControllerPublishVolume, got %d", publishCalled)
	}

	// DetachVolume should skip calling ControllerUnpublishVolume entirely
	if err := plugin.DetachVolume(ctx, "test-vol", "node-1"); err != nil {
		t.Errorf("DetachVolume failed: %v", err)
	}
	if unpublishCalled != 0 {
		t.Errorf("expected 0 calls to ControllerUnpublishVolume, got %d", unpublishCalled)
	}
}

func TestPlugin_Capabilities_SkipStageUnstageWhenUnsupported(t *testing.T) {
	var stageCalled, unstageCalled int
	driver := &mockCSIDriver{
		nodeGetCapabilitiesFunc: func(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
			// Return capabilities WITHOUT STAGE_UNSTAGE_VOLUME
			return &csi.NodeGetCapabilitiesResponse{
				Capabilities: []*csi.NodeServiceCapability{
					{
						Type: &csi.NodeServiceCapability_Rpc{
							Rpc: &csi.NodeServiceCapability_RPC{
								Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
							},
						},
					},
				},
			}, nil
		},
		nodeStageVolumeFunc: func(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
			stageCalled++
			return &csi.NodeStageVolumeResponse{}, nil
		},
		nodeUnstageVolumeFunc: func(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
			unstageCalled++
			return &csi.NodeUnstageVolumeResponse{}, nil
		},
	}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	plugin := NewPlugin(client)
	ctx := context.Background()

	plugin.InitNodeCapabilities(ctx)

	if plugin.SupportsNodeCapability(csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME) {
		t.Errorf("expected STAGE_UNSTAGE_VOLUME to be false")
	}

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "target")
	if err := os.MkdirAll(targetPath, 0750); err != nil {
		t.Fatalf("failed to create target path: %v", err)
	}

	// MountVolume should skip NodeStageVolume entirely
	if err := plugin.MountVolume(ctx, volume.MountVolumeRequest{VolumeID: "test-vol", TargetPath: targetPath}); err != nil {
		t.Errorf("MountVolume failed: %v", err)
	}
	if stageCalled != 0 {
		t.Errorf("expected 0 calls to NodeStageVolume, got %d", stageCalled)
	}

	// UnmountVolume should skip NodeUnstageVolume entirely
	if err := plugin.UnmountVolume(ctx, "test-vol", targetPath, volume.ReadWriteOnce); err != nil {
		t.Errorf("UnmountVolume failed: %v", err)
	}
	if unstageCalled != 0 {
		t.Errorf("expected 0 calls to NodeUnstageVolume, got %d", unstageCalled)
	}
}

func TestPlugin_Capabilities_AllEnabledWhenQueryFailsOrEmpty(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "Unimplemented", err: status.Error(codes.Unimplemented, "failed")},
		{name: "Unavailable", err: status.Error(codes.Unavailable, "failed")},
		{name: "Empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			driver := &mockCSIDriver{
				controllerGetCapabilitiesFunc: func(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
					if tc.err != nil {
						return nil, tc.err
					}
					return &csi.ControllerGetCapabilitiesResponse{}, nil
				},
				nodeGetCapabilitiesFunc: func(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
					if tc.err != nil {
						return nil, tc.err
					}
					return &csi.NodeGetCapabilitiesResponse{}, nil
				},
			}
			endpoint, cleanup := startMockCSIDriver(t, driver)
			defer cleanup()

			client, err := NewCSIClient(endpoint, nil)
			if err != nil {
				t.Fatalf("failed to create CSI client: %v", err)
			}
			defer client.Close()

			plugin := NewPlugin(client)
			ctx := context.Background()
			plugin.InitControllerCapabilities(ctx)
			plugin.InitNodeCapabilities(ctx)

			if !plugin.SupportsControllerCapability(csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME) {
				t.Errorf("expected PUBLISH_UNPUBLISH_VOLUME to be true")
			}
			if !plugin.SupportsNodeCapability(csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME) {
				t.Errorf("expected STAGE_UNSTAGE_VOLUME to be true")
			}
		})
	}
}

func TestPlugin_Capabilities_DisableAttachDetachAfterUnimplemented(t *testing.T) {
	var publishCalled, unpublishCalled int
	driver := &mockCSIDriver{
		controllerPublishVolumeFunc: func(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
			publishCalled++
			return nil, status.Error(codes.Unimplemented, "unimplemented")
		},
		controllerUnpublishVolumeFunc: func(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
			unpublishCalled++
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		},
	}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	plugin := NewPlugin(client)
	ctx := context.Background()
	plugin.InitControllerCapabilities(ctx)

	for range 2 {
		if _, err := plugin.AttachVolume(ctx, volume.AttachVolumeRequest{VolumeID: "test-vol", Node: "node-1"}); err != nil {
			t.Errorf("AttachVolume failed: %v", err)
		}
	}
	if publishCalled != 1 {
		t.Errorf("expected 1 call to ControllerPublishVolume, got %d", publishCalled)
	}
	if plugin.SupportsControllerCapability(csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME) {
		t.Errorf("expected PUBLISH_UNPUBLISH_VOLUME to be false after Unimplemented")
	}

	if err := plugin.DetachVolume(ctx, "test-vol", "node-1"); err != nil {
		t.Errorf("DetachVolume failed: %v", err)
	}
	if unpublishCalled != 0 {
		t.Errorf("expected 0 calls to ControllerUnpublishVolume, got %d", unpublishCalled)
	}
}

func TestPlugin_Capabilities_DisableStageUnstageAfterUnimplemented(t *testing.T) {
	var stageCalled, unstageCalled int
	driver := &mockCSIDriver{
		nodeStageVolumeFunc: func(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
			stageCalled++
			return nil, status.Error(codes.Unimplemented, "unimplemented")
		},
		nodeUnstageVolumeFunc: func(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
			unstageCalled++
			return &csi.NodeUnstageVolumeResponse{}, nil
		},
		nodePublishVolumeFunc: func(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
			if req.GetStagingTargetPath() != "" {
				t.Errorf("expected empty StagingTargetPath, got %q", req.GetStagingTargetPath())
			}
			return &csi.NodePublishVolumeResponse{}, nil
		},
	}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()

	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()

	plugin := NewPlugin(client)
	tmpDir := t.TempDir()
	plugin.stagingDirPrefix = filepath.Join(tmpDir, "staging")
	targetPath := filepath.Join(tmpDir, "target")
	ctx := context.Background()
	plugin.InitNodeCapabilities(ctx)

	for range 2 {
		if err := plugin.MountVolume(ctx, volume.MountVolumeRequest{VolumeID: "test-vol", TargetPath: targetPath}); err != nil {
			t.Errorf("MountVolume failed: %v", err)
		}
	}
	if stageCalled != 1 {
		t.Errorf("expected 1 call to NodeStageVolume, got %d", stageCalled)
	}
	if plugin.SupportsNodeCapability(csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME) {
		t.Errorf("expected STAGE_UNSTAGE_VOLUME to be false after Unimplemented")
	}
	stagingPath := filepath.Join(plugin.stagingDirPrefix, "test-vol")
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Errorf("expected staging directory %q to be removed, got err: %v", stagingPath, err)
	}

	if err := plugin.UnmountVolume(ctx, "test-vol", targetPath, volume.ReadWriteOnce); err != nil {
		t.Errorf("UnmountVolume failed: %v", err)
	}
	if unstageCalled != 0 {
		t.Errorf("expected 0 calls to NodeUnstageVolume, got %d", unstageCalled)
	}
}

func TestNewCSIPlugin_CapabilityQueryFailureIsNotFatal(t *testing.T) {
	for _, isController := range []bool{true, false} {
		t.Run(fmt.Sprintf("isController=%t", isController), func(t *testing.T) {
			driver := &mockCSIDriver{
				controllerGetCapabilitiesFunc: func(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
					return nil, status.Error(codes.Unavailable, "unavailable")
				},
				nodeGetCapabilitiesFunc: func(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
					return nil, status.Error(codes.Unavailable, "unavailable")
				},
			}
			endpoint, cleanup := startMockCSIDriver(t, driver)
			defer cleanup()

			cfg := driverConfig(endpoint, nil)
			cfg.Spec.NodeSocketOverride = endpoint
			plugin, err := newCSIPlugin(t.Context(), &mockLister{cfg: cfg}, mockDriverName, isController, tlsPaths{})
			if err != nil {
				t.Fatalf("newCSIPlugin failed: %v", err)
			}
			defer plugin.client.Close()

			if isController && !plugin.SupportsControllerCapability(csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME) {
				t.Errorf("expected PUBLISH_UNPUBLISH_VOLUME to be true")
			}
			if !isController && !plugin.SupportsNodeCapability(csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME) {
				t.Errorf("expected STAGE_UNSTAGE_VOLUME to be true")
			}
		})
	}
}

func TestPlugin_MountVolumeMultiNode(t *testing.T) {
	var staged, unstaged []string
	var published []*csi.NodePublishVolumeRequest
	driver := &mockCSIDriver{
		nodeStageVolumeFunc: func(_ context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
			staged = append(staged, req.GetStagingTargetPath())
			return &csi.NodeStageVolumeResponse{}, nil
		},
		nodePublishVolumeFunc: func(_ context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
			published = append(published, req)
			return &csi.NodePublishVolumeResponse{}, nil
		},
		nodeUnstageVolumeFunc: func(_ context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
			unstaged = append(unstaged, req.GetStagingTargetPath())
			return &csi.NodeUnstageVolumeResponse{}, nil
		},
		nodeGetCapabilitiesFunc: func(context.Context, *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
			return &csi.NodeGetCapabilitiesResponse{Capabilities: []*csi.NodeServiceCapability{{
				Type: &csi.NodeServiceCapability_Rpc{Rpc: &csi.NodeServiceCapability_RPC{Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME}},
			}}}, nil
		},
	}
	endpoint, cleanup := startMockCSIDriver(t, driver)
	defer cleanup()
	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	defer client.Close()
	plugin := NewPlugin(client)
	tmpDir := t.TempDir()
	plugin.stagingDirPrefix = filepath.Join(tmpDir, "staging")
	ctx := context.Background()
	plugin.InitNodeCapabilities(ctx)

	// Two actors on one node mount the same read-write-many volume.
	targetA, targetB := filepath.Join(tmpDir, "a"), filepath.Join(tmpDir, "b")
	for _, target := range []string{targetA, targetB} {
		if err := plugin.MountVolume(ctx, volume.MountVolumeRequest{VolumeID: "shared", TargetPath: target, Mode: volume.ReadWriteMany}); err != nil {
			t.Fatalf("MountVolume %s: %v", target, err)
		}
	}
	shared := filepath.Join(plugin.stagingDirPrefix, "shared", sharedStagingDir)
	if len(staged) != 2 || staged[0] != shared || staged[1] != shared {
		t.Fatalf("staging paths = %v, want %q for both targets", staged, shared)
	}
	for _, req := range published {
		if got := req.GetVolumeCapability().GetAccessMode().GetMode(); got != csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
			t.Errorf("publish access mode = %v, want MULTI_NODE_MULTI_WRITER", got)
		}
		if req.GetReadonly() {
			t.Errorf("read-write-many volume published read-only")
		}
	}

	// One actor's unmount leaves the volume staged for the other; the last
	// one unstages it.
	if err := plugin.UnmountVolume(ctx, "shared", targetA, volume.ReadWriteMany); err != nil {
		t.Fatalf("UnmountVolume: %v", err)
	}
	if len(unstaged) != 0 {
		t.Errorf("unstaged = %v after the first of two targets, want nothing", unstaged)
	}
	if err := plugin.UnmountVolume(ctx, "shared", targetB, volume.ReadWriteMany); err != nil {
		t.Fatalf("UnmountVolume: %v", err)
	}
	if len(unstaged) != 1 || unstaged[0] != shared {
		t.Errorf("unstaged = %v after the last target, want %q", unstaged, shared)
	}
	if _, err := os.Stat(filepath.Join(plugin.stagingDirPrefix, "shared")); !os.IsNotExist(err) {
		t.Errorf("the volume's staging directory is left after its last target: %v", err)
	}

	// A read-only-many volume is published read-only.
	published = nil
	if err := plugin.MountVolume(ctx, volume.MountVolumeRequest{VolumeID: "mirrors", TargetPath: targetA, Mode: volume.ReadOnlyMany}); err != nil {
		t.Fatalf("MountVolume: %v", err)
	}
	if req := published[0]; !req.GetReadonly() || req.GetVolumeCapability().GetAccessMode().GetMode() != csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
		t.Errorf("read-only-many publish = %v, want read-only MULTI_NODE_READER_ONLY", req)
	}

	// A read-write-many volume mounted read-only is staged and published with
	// its own mode and the readonly flag, as the kubelet does: a driver may
	// refuse MULTI_NODE_READER_ONLY for it.
	published, staged = nil, nil
	var stageModes []csi.VolumeCapability_AccessMode_Mode
	driver.nodeStageVolumeFunc = func(_ context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
		stageModes = append(stageModes, req.GetVolumeCapability().GetAccessMode().GetMode())
		return &csi.NodeStageVolumeResponse{}, nil
	}
	if err := plugin.MountVolume(ctx, volume.MountVolumeRequest{VolumeID: "efs", TargetPath: targetB, Mode: volume.ReadWriteMany, ReadOnly: true}); err != nil {
		t.Fatalf("MountVolume: %v", err)
	}
	if len(stageModes) != 1 || stageModes[0] != csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
		t.Errorf("stage access modes = %v, want MULTI_NODE_MULTI_WRITER", stageModes)
	}
	if req := published[0]; !req.GetReadonly() || req.GetVolumeCapability().GetAccessMode().GetMode() != csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
		t.Errorf("read-only publish of a read-write-many volume = %v, want read-only MULTI_NODE_MULTI_WRITER", req)
	}
}

// strictStagingDriver stages a volume at one path per node and refuses
// another, as csi-driver-host-path does and the CSI spec asks of the
// orchestrator.
func strictStagingDriver(staged map[string]string, unstaged *[]string, publish func(*csi.NodePublishVolumeRequest) error) *mockCSIDriver {
	return &mockCSIDriver{
		nodeStageVolumeFunc: func(_ context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
			if at, ok := staged[req.GetVolumeId()]; ok && at != req.GetStagingTargetPath() {
				return nil, status.Errorf(codes.FailedPrecondition, "volume %q is already staged at %q", req.GetVolumeId(), at)
			}
			staged[req.GetVolumeId()] = req.GetStagingTargetPath()
			return &csi.NodeStageVolumeResponse{}, nil
		},
		nodeUnstageVolumeFunc: func(_ context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
			*unstaged = append(*unstaged, req.GetStagingTargetPath())
			if staged[req.GetVolumeId()] == req.GetStagingTargetPath() {
				delete(staged, req.GetVolumeId())
			}
			return &csi.NodeUnstageVolumeResponse{}, nil
		},
		nodePublishVolumeFunc: func(_ context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
			if publish != nil {
				if err := publish(req); err != nil {
					return nil, err
				}
			}
			return &csi.NodePublishVolumeResponse{}, nil
		},
	}
}

func newStagingTestPlugin(t *testing.T, driver *mockCSIDriver, stagingDir string) *Plugin {
	t.Helper()
	endpoint, cleanup := startMockCSIDriver(t, driver)
	t.Cleanup(cleanup)
	client, err := NewCSIClient(endpoint, nil)
	if err != nil {
		t.Fatalf("failed to create CSI client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	plugin := NewPlugin(client)
	plugin.stagingDirPrefix = stagingDir
	plugin.InitNodeCapabilities(t.Context())
	return plugin
}

// One actor mounting a read-write-many volume twice (its own directory and
// the mirrors) and a second actor on the same node mounting it too are all
// served by one staging, which a driver that allows one staging path per
// volume accepts; the staging outlives every unmount but the last, across a
// restart of the process in between.
func TestPlugin_MultiNodeVolumeStagedOncePerNode(t *testing.T) {
	staged := map[string]string{}
	var unstaged []string
	driver := strictStagingDriver(staged, &unstaged, nil)
	dir := filepath.Join(t.TempDir(), "staging")
	plugin := newStagingTestPlugin(t, driver, dir)
	ctx := t.Context()

	targets := []string{"/actors/a/session", "/actors/a/mirrors", "/actors/b/session"}
	for i, target := range targets {
		req := volume.MountVolumeRequest{VolumeID: "workspace", TargetPath: target, Mode: volume.ReadWriteMany, ReadOnly: i == 1}
		if err := plugin.MountVolume(ctx, req); err != nil {
			t.Fatalf("MountVolume %s: %v", target, err)
		}
	}
	if want := filepath.Join(dir, "workspace", sharedStagingDir); staged["workspace"] != want {
		t.Fatalf("staged at %q, want %q", staged["workspace"], want)
	}

	restarted := newStagingTestPlugin(t, driver, dir)
	for _, target := range targets[:2] {
		if err := restarted.UnmountVolume(ctx, "workspace", target, volume.ReadWriteMany); err != nil {
			t.Fatalf("UnmountVolume %s: %v", target, err)
		}
	}
	if len(unstaged) != 0 {
		t.Fatalf("unstaged %v while %s still has the volume", unstaged, targets[2])
	}
	if err := restarted.UnmountVolume(ctx, "workspace", targets[2], volume.ReadWriteMany); err != nil {
		t.Fatalf("UnmountVolume %s: %v", targets[2], err)
	}
	if len(unstaged) != 1 || len(staged) != 0 {
		t.Errorf("after the last target: unstaged %v, still staged %v; want one unstage and nothing staged", unstaged, staged)
	}
}

// A target a release before the shared layout staged at its own path is
// unstaged there, leaving the shared staging of the targets mounted since.
func TestPlugin_MultiNodeVolumeLegacyStagingUnstagedWhereItIs(t *testing.T) {
	staged := map[string]string{}
	var unstaged []string
	dir := filepath.Join(t.TempDir(), "staging")
	plugin := newStagingTestPlugin(t, strictStagingDriver(staged, &unstaged, nil), dir)
	ctx := t.Context()

	legacy := filepath.Join(dir, "workspace", targetKey("/actors/old/session"))
	if err := os.MkdirAll(legacy, 0750); err != nil {
		t.Fatal(err)
	}
	if err := plugin.UnmountVolume(ctx, "workspace", "/actors/old/session", volume.ReadWriteMany); err != nil {
		t.Fatalf("UnmountVolume: %v", err)
	}
	if len(unstaged) != 1 || unstaged[0] != legacy {
		t.Errorf("unstaged %v, want the legacy staging %q", unstaged, legacy)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy staging directory left: %v", err)
	}
}

// A publish that fails releases the target's hold: the last holder gone, the
// volume is unstaged.
func TestPlugin_MultiNodeVolumeFailedPublishReleasesStaging(t *testing.T) {
	staged := map[string]string{}
	var unstaged []string
	refuse := func(*csi.NodePublishVolumeRequest) error { return status.Error(codes.Internal, "refused") }
	dir := filepath.Join(t.TempDir(), "staging")
	plugin := newStagingTestPlugin(t, strictStagingDriver(staged, &unstaged, refuse), dir)

	err := plugin.MountVolume(t.Context(), volume.MountVolumeRequest{VolumeID: "workspace", TargetPath: "/actors/a/session", Mode: volume.ReadWriteMany})
	if err == nil {
		t.Fatal("MountVolume succeeded, want the publish's refusal")
	}
	if len(staged) != 0 || len(unstaged) != 1 {
		t.Errorf("after a failed publish: staged %v, unstaged %v; want the volume unstaged", staged, unstaged)
	}
	if _, err := os.Stat(filepath.Join(dir, "workspace")); !os.IsNotExist(err) {
		t.Errorf("the volume's staging directory is left after a failed publish: %v", err)
	}
}

// Targets mounting and unmounting one volume at the same time stage it once
// and unstage it once, after the last.
func TestPlugin_MultiNodeVolumeConcurrentTargets(t *testing.T) {
	var mu sync.Mutex
	stages, unstages := 0, 0
	driver := &mockCSIDriver{
		nodeStageVolumeFunc: func(context.Context, *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			stages++
			return &csi.NodeStageVolumeResponse{}, nil
		},
		nodeUnstageVolumeFunc: func(context.Context, *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			unstages++
			return &csi.NodeUnstageVolumeResponse{}, nil
		},
	}
	plugin := newStagingTestPlugin(t, driver, filepath.Join(t.TempDir(), "staging"))
	ctx := t.Context()

	const n = 16
	run := func(op func(target string) error) {
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := range n {
			wg.Go(func() { errs <- op(fmt.Sprintf("/actors/%d/session", i)) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	run(func(target string) error {
		return plugin.MountVolume(ctx, volume.MountVolumeRequest{VolumeID: "workspace", TargetPath: target, Mode: volume.ReadWriteMany})
	})
	run(func(target string) error {
		return plugin.UnmountVolume(ctx, "workspace", target, volume.ReadWriteMany)
	})
	if stages != n || unstages != 1 {
		t.Errorf("stages %d, unstages %d; want %d idempotent stages and one unstage", stages, unstages, n)
	}
}
