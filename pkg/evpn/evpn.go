// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.

// Package evpn is the main package of the application
package evpn

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"path"
	"strconv"

	"github.com/vishvananda/netlink"

	pb "github.com/opiproject/opi-api/network/cloud/v1alpha1/gen/go"
	pc "github.com/opiproject/opi-api/network/opinetcommon/v1alpha1/gen/go"

	"go.einride.tech/aip/fieldbehavior"
	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/resourceid"
	"go.einride.tech/aip/resourcename"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Server represents the Server object
type Server struct {
	pb.UnimplementedCloudInfraServiceServer
	Subnets    map[string]*pb.Subnet
	Interfaces map[string]*pb.Interface
	Vpcs       map[string]*pb.Vpc
}

// CreateVpc executes the creation of the VRF/VPC
func (s *Server) CreateVpc(_ context.Context, in *pb.CreateVpcRequest) (*pb.Vpc, error) {
	log.Printf("CreateVpc: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// see https://google.aip.dev/133#user-specified-ids
	resourceID := resourceid.NewSystemGenerated()
	if in.VpcId != "" {
		err := resourceid.ValidateUserSettable(in.VpcId)
		if err != nil {
			log.Printf("error: %v", err)
			return nil, err
		}
		log.Printf("client provided the ID of a resource %v, ignoring the name field %v", in.VpcId, in.Vpc.Name)
		resourceID = in.VpcId
	}
	in.Vpc.Name = fmt.Sprintf("//network.opiproject.org/vpcs/%s", resourceID)
	// idempotent API when called with same key, should return same object
	obj, ok := s.Vpcs[in.Vpc.Name]
	if ok {
		log.Printf("Already existing Vpc with id %v", in.Vpc.Name)
		return obj, nil
	}
	// TODO: search DB for tables by this reference instead of reinterpreting this as integer
	table, err := strconv.Atoi(in.Vpc.Spec.V4RouteTableNameRef)
	if err != nil {
		err := status.Error(codes.InvalidArgument, "could not convert V4RouteTableNameRef to integer")
		log.Printf("error: %v", err)
		return nil, err
	}
	// not found, so create a new one
	vrf := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: resourceID}, Table: uint32(table)}
	if err := netlink.LinkAdd(vrf); err != nil {
		fmt.Printf("Failed to create link: %v", err)
		return nil, err
	}
	if err := netlink.LinkSetUp(vrf); err != nil {
		fmt.Printf("Failed to up link: %v", err)
		return nil, err
	}
	// TODO: replace cloud -> evpn
	response := proto.Clone(in.Vpc).(*pb.Vpc)
	response.Status = &pb.VpcStatus{SubnetCount: 4}
	s.Vpcs[in.Vpc.Name] = response
	log.Printf("CreateVpc: Sending to client: %v", response)
	return response, nil
}

// DeleteVpc deletes a VRF/VPC
func (s *Server) DeleteVpc(_ context.Context, in *pb.DeleteVpcRequest) (*emptypb.Empty, error) {
	log.Printf("DeleteVpc: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// Validate that a resource name conforms to the restrictions outlined in AIP-122.
	if err := resourcename.Validate(in.Name); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// fetch object from the database
	obj, ok := s.Vpcs[in.Name]
	if !ok {
		if in.AllowMissing {
			return &emptypb.Empty{}, nil
		}
		err := status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
		log.Printf("error: %v", err)
		return nil, err
	}
	resourceID := path.Base(obj.Name)
	// use netlink to find VRF/VPC
	vrf, err := netlink.LinkByName(resourceID)
	if err != nil {
		err := status.Errorf(codes.NotFound, "unable to find key %s", resourceID)
		log.Printf("error: %v", err)
		return nil, err
	}
	// bring link down
	if err := netlink.LinkSetDown(vrf); err != nil {
		fmt.Printf("Failed to up link: %v", err)
		return nil, err
	}
	// use netlink to delete VRF/VPC
	if err := netlink.LinkDel(vrf); err != nil {
		fmt.Printf("Failed to delete link: %v", err)
		return nil, err
	}
	// remove from the Database
	delete(s.Vpcs, obj.Name)
	return &emptypb.Empty{}, nil
}

// UpdateVpc updates an VRF/VPC
func (s *Server) UpdateVpc(_ context.Context, in *pb.UpdateVpcRequest) (*pb.Vpc, error) {
	log.Printf("UpdateVpc: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// Validate that a resource name conforms to the restrictions outlined in AIP-122.
	if err := resourcename.Validate(in.Vpc.Name); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// fetch object from the database
	volume, ok := s.Vpcs[in.Vpc.Name]
	if !ok {
		// TODO: introduce "in.AllowMissing" field. In case "true", create a new resource, don't return error
		err := status.Errorf(codes.NotFound, "unable to find key %s", in.Vpc.Name)
		log.Printf("error: %v", err)
		return nil, err
	}
	resourceID := path.Base(volume.Name)
	// update_mask = 2
	if err := fieldmask.Validate(in.UpdateMask, in.Vpc); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	log.Printf("TODO: use resourceID=%v", resourceID)
	return nil, status.Errorf(codes.Unimplemented, "UpdateVpc method is not implemented")
}

// GetVpc gets an VRF/VPC
func (s *Server) GetVpc(_ context.Context, in *pb.GetVpcRequest) (*pb.Vpc, error) {
	log.Printf("GetVpc: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// Validate that a resource name conforms to the restrictions outlined in AIP-122.
	if err := resourcename.Validate(in.Name); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// fetch object from the database
	obj, ok := s.Vpcs[in.Name]
	if !ok {
		err := status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
		log.Printf("error: %v", err)
		return nil, err
	}
	resourceID := path.Base(obj.Name)
	_, err := netlink.LinkByName(resourceID)
	if err != nil {
		err := status.Errorf(codes.NotFound, "unable to find key %s", resourceID)
		log.Printf("error: %v", err)
		return nil, err
	}
	// TODO
	return &pb.Vpc{Name: in.Name, Spec: &pb.VpcSpec{V4RouteTableNameRef: obj.Spec.V4RouteTableNameRef, Tos: 11}, Status: &pb.VpcStatus{SubnetCount: 77}}, nil
}

// CreateSubnet executes the creation of the SVI/subnet
func (s *Server) CreateSubnet(_ context.Context, in *pb.CreateSubnetRequest) (*pb.Subnet, error) {
	log.Printf("CreateSubnet: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// see https://google.aip.dev/133#user-specified-ids
	resourceID := resourceid.NewSystemGenerated()
	if in.SubnetId != "" {
		err := resourceid.ValidateUserSettable(in.SubnetId)
		if err != nil {
			log.Printf("error: %v", err)
			return nil, err
		}
		log.Printf("client provided the ID of a resource %v, ignoring the name field %v", in.SubnetId, in.Subnet.Name)
		resourceID = in.SubnetId
	}
	in.Subnet.Name = fmt.Sprintf("//network.opiproject.org/subnets/%s", resourceID)
	// idempotent API when called with same key, should return same object
	snet, ok := s.Subnets[in.Subnet.Name]
	if ok {
		log.Printf("Already existing Subnet with id %v", in.Subnet.Name)
		return snet, nil
	}
	// not found, so create a new one
	bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: resourceID}}
	if err := netlink.LinkAdd(bridge); err != nil {
		fmt.Printf("Failed to create link: %v", err)
		return nil, err
	}
	// set MAC
	mac := in.Subnet.Spec.VirtualRouterMac
	if len(mac) > 0 {
		if err := netlink.LinkSetHardwareAddr(bridge, mac); err != nil {
			fmt.Printf("Failed to set MAC on link: %v", err)
			return nil, err
		}
	}
	// set IPv4
	if in.Subnet.Spec.V4Prefix.Addr > 0 && in.Subnet.Spec.V4Prefix.Len > 0 {
		myip := make(net.IP, 4)
		binary.BigEndian.PutUint32(myip, in.Subnet.Spec.V4Prefix.Addr)
		addr := &netlink.Addr{IPNet: &net.IPNet{IP: myip, Mask: net.CIDRMask(int(in.Subnet.Spec.V4Prefix.Len), 32)}}
		if err := netlink.AddrAdd(bridge, addr); err != nil {
			fmt.Printf("Failed to set IP on link: %v", err)
			return nil, err
		}
	}
	// set UP
	if err := netlink.LinkSetUp(bridge); err != nil {
		fmt.Printf("Failed to up link: %v", err)
		return nil, err
	}
	// TODO: plug interfaces into this bridge ?
	// vpc, ok := s.Vpcs[in.Subnet.Spec.VpcNameRef]
	// if !ok {
	// 	err := status.Errorf(codes.NotFound, "unable to find key %s", in.Subnet.Spec.VpcNameRef)
	// 	log.Printf("error: %v", err)
	// 	return nil, err
	// }
	// iface, err := netlink.LinkByName(path.Base(vpc.Name))
	// if err != nil {
	// 	err := status.Errorf(codes.NotFound, "unable to find key %s", vpc.Name)
	// 	log.Printf("error: %v", err)
	// 	return nil, err
	// }
	// if err := netlink.LinkSetMaster(iface, bridge); err != nil {
	// 	fmt.Printf("Failed to add iface to bridge: %v", err)
	// 	return nil, err
	// }
	// TODO: replace cloud -> evpn
	response := proto.Clone(in.Subnet).(*pb.Subnet)
	response.Status = &pb.SubnetStatus{HwIndex: 8, VnicCount: 88}
	s.Subnets[in.Subnet.Name] = response
	log.Printf("CreateSubnet: Sending to client: %v", response)
	return response, nil
}

// DeleteSubnet deletes a SVI/subnet
func (s *Server) DeleteSubnet(_ context.Context, in *pb.DeleteSubnetRequest) (*emptypb.Empty, error) {
	log.Printf("DeleteSubnet: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// Validate that a resource name conforms to the restrictions outlined in AIP-122.
	if err := resourcename.Validate(in.Name); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// fetch object from the database
	obj, ok := s.Subnets[in.Name]
	if !ok {
		if in.AllowMissing {
			return &emptypb.Empty{}, nil
		}
		err := status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
		log.Printf("error: %v", err)
		return nil, err
	}
	resourceID := path.Base(obj.Name)
	// use netlink to find VRF/VPC
	vrf, err := netlink.LinkByName(resourceID)
	if err != nil {
		err := status.Errorf(codes.NotFound, "unable to find key %s", resourceID)
		log.Printf("error: %v", err)
		return nil, err
	}
	// bring link down
	if err := netlink.LinkSetDown(vrf); err != nil {
		fmt.Printf("Failed to up link: %v", err)
		return nil, err
	}
	// use netlink to delete VRF/VPC
	if err := netlink.LinkDel(vrf); err != nil {
		fmt.Printf("Failed to delete link: %v", err)
		return nil, err
	}
	// remove from the Database
	delete(s.Subnets, obj.Name)
	return &emptypb.Empty{}, nil
}

// UpdateSubnet updates an SVI/subnet
func (s *Server) UpdateSubnet(_ context.Context, in *pb.UpdateSubnetRequest) (*pb.Subnet, error) {
	log.Printf("UpdateSubnet: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// Validate that a resource name conforms to the restrictions outlined in AIP-122.
	if err := resourcename.Validate(in.Subnet.Name); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// fetch object from the database
	volume, ok := s.Subnets[in.Subnet.Name]
	if !ok {
		// TODO: introduce "in.AllowMissing" field. In case "true", create a new resource, don't return error
		err := status.Errorf(codes.NotFound, "unable to find key %s", in.Subnet.Name)
		log.Printf("error: %v", err)
		return nil, err
	}
	resourceID := path.Base(volume.Name)
	// update_mask = 2
	if err := fieldmask.Validate(in.UpdateMask, in.Subnet); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	log.Printf("TODO: use resourceID=%v", resourceID)
	return nil, status.Errorf(codes.Unimplemented, "UpdateSubnet method is not implemented")
}

// GetSubnet gets an SVI/subnet
func (s *Server) GetSubnet(_ context.Context, in *pb.GetSubnetRequest) (*pb.Subnet, error) {
	log.Printf("GetSubnet: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// Validate that a resource name conforms to the restrictions outlined in AIP-122.
	if err := resourcename.Validate(in.Name); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// fetch object from the database
	snet, ok := s.Subnets[in.Name]
	if !ok {
		err := status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
		log.Printf("error: %v", err)
		return nil, err
	}
	resourceID := path.Base(snet.Name)
	_, err := netlink.LinkByName(resourceID)
	if err != nil {
		err := status.Errorf(codes.NotFound, "unable to find key %s", resourceID)
		log.Printf("error: %v", err)
		return nil, err
	}
	// TODO
	return &pb.Subnet{Name: in.Name, Spec: &pb.SubnetSpec{Ipv4VirtualRouterIp: snet.Spec.Ipv4VirtualRouterIp}, Status: &pb.SubnetStatus{HwIndex: 77}}, nil
}

// CreateInterface executes the creation of the interface
func (s *Server) CreateInterface(_ context.Context, in *pb.CreateInterfaceRequest) (*pb.Interface, error) {
	log.Printf("CreateInterface: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// see https://google.aip.dev/133#user-specified-ids
	resourceID := resourceid.NewSystemGenerated()
	if in.InterfaceId != "" {
		err := resourceid.ValidateUserSettable(in.InterfaceId)
		if err != nil {
			log.Printf("error: %v", err)
			return nil, err
		}
		log.Printf("client provided the ID of a resource %v, ignoring the name field %v", in.InterfaceId, in.Interface.Name)
		resourceID = in.InterfaceId
	}
	in.Interface.Name = fmt.Sprintf("//network.opiproject.org/interfaces/%s", resourceID)
	// idempotent API when called with same key, should return same object
	iface, ok := s.Interfaces[in.Interface.Name]
	if ok {
		log.Printf("Already existing Interface with id %v", in.Interface.Name)
		return iface, nil
	}
	// create dummy interface
	dummy := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: resourceID}}
	if err := netlink.LinkAdd(dummy); err != nil {
		fmt.Printf("Failed to create link: %v", err)
		return nil, err
	}
	switch u := in.Interface.Spec.Ifinfo.(type) {
	case *pb.InterfaceSpec_ControlIfSpec:
		// set MAC
		mac := u.ControlIfSpec.MacAddress
		if len(mac) > 0 {
			if err := netlink.LinkSetHardwareAddr(dummy, mac); err != nil {
				fmt.Printf("Failed to set MAC on link: %v", err)
				return nil, err
			}
		}
		// set IPv4
		prefix := u.ControlIfSpec.Prefix
		switch a := prefix.Addr.V4OrV6.(type) {
		case *pc.IPAddress_V4Addr:
			if prefix.Addr.Af == pc.IpAf_IP_AF_INET && prefix.Len > 0 && a.V4Addr > 0 {
				myip := make(net.IP, 4)
				binary.BigEndian.PutUint32(myip, a.V4Addr)
				addr := &netlink.Addr{IPNet: &net.IPNet{IP: myip, Mask: net.CIDRMask(int(prefix.Len), 32)}}
				if err := netlink.AddrAdd(dummy, addr); err != nil {
					fmt.Printf("Failed to set IP on link: %v", err)
					return nil, err
				}
			}
		default:
			fmt.Println("No matching operations")
		}
	default:
		fmt.Println("No matching operations")
	}
	if err := netlink.LinkSetUp(dummy); err != nil {
		fmt.Printf("Failed to up link: %v", err)
		return nil, err
	}
	// TODO: do we also need to add this link to the bridge here ? search s.Subnets ?
	response := proto.Clone(in.Interface).(*pb.Interface)
	response.Status = &pb.InterfaceStatus{IfIndex: 8, OperStatus: pb.IfStatus_IF_STATUS_UP}
	s.Interfaces[in.Interface.Name] = response
	log.Printf("CreateInterface: Sending to client: %v", response)
	return response, nil
}

// DeleteInterface deletes an interface
func (s *Server) DeleteInterface(_ context.Context, in *pb.DeleteInterfaceRequest) (*emptypb.Empty, error) {
	log.Printf("DeleteInterface: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// Validate that a resource name conforms to the restrictions outlined in AIP-122.
	if err := resourcename.Validate(in.Name); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// fetch object from the database
	iface, ok := s.Interfaces[in.Name]
	if !ok {
		if in.AllowMissing {
			return &emptypb.Empty{}, nil
		}
		err := status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
		log.Printf("error: %v", err)
		return nil, err
	}
	resourceID := path.Base(iface.Name)
	// use netlink to find interface
	dummy, err := netlink.LinkByName(resourceID)
	if err != nil {
		err := status.Errorf(codes.NotFound, "unable to find key %s", resourceID)
		log.Printf("error: %v", err)
		return nil, err
	}
	// bring link down
	if err := netlink.LinkSetDown(dummy); err != nil {
		fmt.Printf("Failed to up link: %v", err)
		return nil, err
	}
	// use netlink to delete dummy interface
	if err := netlink.LinkDel(dummy); err != nil {
		fmt.Printf("Failed to delete link: %v", err)
		return nil, err
	}
	// remove from the Database
	delete(s.Interfaces, iface.Name)
	return &emptypb.Empty{}, nil
}

// UpdateInterface updates an Nvme Subsystem
func (s *Server) UpdateInterface(_ context.Context, in *pb.UpdateInterfaceRequest) (*pb.Interface, error) {
	log.Printf("UpdateInterface: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// Validate that a resource name conforms to the restrictions outlined in AIP-122.
	if err := resourcename.Validate(in.Interface.Name); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// fetch object from the database
	volume, ok := s.Interfaces[in.Interface.Name]
	if !ok {
		// TODO: introduce "in.AllowMissing" field. In case "true", create a new resource, don't return error
		err := status.Errorf(codes.NotFound, "unable to find key %s", in.Interface.Name)
		log.Printf("error: %v", err)
		return nil, err
	}
	resourceID := path.Base(volume.Name)
	// update_mask = 2
	if err := fieldmask.Validate(in.UpdateMask, in.Interface); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	log.Printf("TODO: use resourceID=%v", resourceID)
	// TODO: modify dummy interface
	// dummy := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: resourceID}}
	// if err := netlink.LinkModify(dummy); err != nil {
	// 	fmt.Printf("Failed to delete link: %v", err)
	// 	return nil, err
	// }
	return nil, status.Errorf(codes.Unimplemented, "UpdateInterface method is not implemented")
}

// GetInterface gets an Interface
func (s *Server) GetInterface(_ context.Context, in *pb.GetInterfaceRequest) (*pb.Interface, error) {
	log.Printf("GetInterface: Received from client: %v", in)
	// check required fields
	if err := fieldbehavior.ValidateRequiredFields(in); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// Validate that a resource name conforms to the restrictions outlined in AIP-122.
	if err := resourcename.Validate(in.Name); err != nil {
		log.Printf("error: %v", err)
		return nil, err
	}
	// fetch object from the database
	snet, ok := s.Interfaces[in.Name]
	if !ok {
		err := status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
		log.Printf("error: %v", err)
		return nil, err
	}
	resourceID := path.Base(snet.Name)
	_, err := netlink.LinkByName(resourceID)
	if err != nil {
		err := status.Errorf(codes.NotFound, "unable to find key %s", resourceID)
		log.Printf("error: %v", err)
		return nil, err
	}
	// TODO
	return &pb.Interface{Name: in.Name, Spec: &pb.InterfaceSpec{Ifid: snet.Spec.Ifid}, Status: &pb.InterfaceStatus{IfIndex: 55}}, nil
}
