// SPDX-License-Identifier: MIT
pragma solidity ^0.8.30;

import {OpenRegistrar, Grant, IVerifiableFactory, IUserRegistryInit} from "../src/OpenRegistrar.sol";

/// @dev The Foundry cheatcodes these tests use, without a forge-std dependency.
interface Vm {
    function envOr(string calldata name, string calldata defaultValue) external view returns (string memory);
    function createSelectFork(string calldata urlOrAlias) external returns (uint256);
    function createSelectFork(string calldata urlOrAlias, uint256 blockNumber) external returns (uint256);
    function prank(address sender) external;
    function startPrank(address sender) external;
    function stopPrank() external;
    function expectRevert(bytes calldata revertData) external;
    function expectRevert() external;
    function skip(bool skipTest) external;
}

interface IRegistry {
    function register(string calldata, address, address, address, uint256, uint64) external returns (uint256);
    function getSubregistry(string calldata label) external view returns (address);
    function getResolver(string calldata label) external view returns (address);
    function findOwner(string calldata label) external view returns (address);
    function grantRootRoles(uint256 roleBitmap, address account) external returns (bool);
    function revokeRootRoles(uint256 roleBitmap, address account) external returns (bool);
    function hasRootRoles(uint256 roleBitmap, address account) external view returns (bool);
    function roleCount(uint256 resource) external view returns (uint256);
}

interface IResolver {
    function setText(bytes calldata name, string calldata key, string calldata value) external;
    function resolve(bytes calldata name, bytes calldata data) external view returns (bytes memory);
    function hasRootRoles(uint256 roleBitmap, address account) external view returns (bool);
}

/// @notice Runs against a Sepolia fork, so it exercises the real ENSv2 implementations.
///         Set SEPOLIA_RPC_URL to a node that serves recent state; the test skips without one.
contract OpenRegistrarTest {
    Vm constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    // ENSv2 Sepolia deployment (docs/reference/ens-deployments.md).
    address constant FACTORY = 0xDa70306C98E97eCe36F997a21368e53298572991;
    address constant USER_REGISTRY_IMPL = 0x9BD8a88719068D09ecee662f36C0E3856708366a;
    address constant RESOLVER_IMPL = 0x115eb53F0c60696633855F90b138178Fb40b2b2C;

    uint256 constant ALL = 0x1111111111111111111111111111111111111111111111111111111111111111;
    uint256 constant ROLE_REGISTRAR = 1;

    address constant ALICE = address(0xA11CE);
    address constant BOB = address(0xB0B);

    IRegistry nets;
    OpenRegistrar registrar;

    function setUp() public {
        string memory rpc = vm.envOr("SEPOLIA_RPC_URL", string(""));
        if (bytes(rpc).length == 0) {
            vm.skip(true);
            return;
        }
        // Pin a block behind the head: load-balanced public nodes may not have the newest one yet.
        vm.createSelectFork(rpc);
        vm.createSelectFork(rpc, block.number - 32);
        Grant[] memory grants = new Grant[](1);
        grants[0] = Grant(address(this), ALL);
        nets = IRegistry(
            IVerifiableFactory(FACTORY)
                .deployProxy(
                    USER_REGISTRY_IMPL,
                    uint256(keccak256("nets-test")),
                    abi.encodeCall(IUserRegistryInit.initialize, (grants))
                )
        );
        registrar = new OpenRegistrar(address(nets), FACTORY, USER_REGISTRY_IMPL, RESOLVER_IMPL);
        nets.grantRootRoles(ROLE_REGISTRAR, address(registrar));
        // The deployer steps back, as the deploy script does: nobody keeps root on the open registry.
        nets.revokeRootRoles(ALL, address(this));
    }

    function test_claimWithSubregistryGivesTheCallerEverything() public {
        vm.prank(ALICE);
        (address sub, address resolver,) = registrar.claimWithSubregistry("alice-net");

        require(nets.findOwner("alice-net") == ALICE, "owner");
        require(nets.getSubregistry("alice-net") == sub, "subregistry");
        require(nets.getResolver("alice-net") == resolver, "resolver");

        // The caller administers both contracts; the registrar keeps nothing.
        require(IRegistry(sub).hasRootRoles(ALL, ALICE), "alice root on subregistry");
        require(IResolver(resolver).hasRootRoles(ALL, ALICE), "alice root on resolver");
        require(!IRegistry(sub).hasRootRoles(ROLE_REGISTRAR, address(registrar)), "registrar on subregistry");
        require(!IResolver(resolver).hasRootRoles(1 << 4, address(registrar)), "registrar on resolver");

        // Alice enrolls a device and writes its record herself.
        vm.startPrank(ALICE);
        IRegistry(sub).register("laptop", ALICE, address(0), resolver, 0, uint64(block.timestamp + 30 days));
        bytes memory dns = _dns("laptop", "alice-net");
        IResolver(resolver).setText(dns, "pubkey", "abc");
        vm.stopPrank();
        bytes memory answer =
            IResolver(resolver).resolve(dns, abi.encodeWithSignature("text(bytes32,string)", _namehash(dns), "pubkey"));
        require(keccak256(bytes(abi.decode(answer, (string)))) == keccak256("abc"), "text");
    }

    function test_strangerCannotWriteAClaimedTailnet() public {
        vm.prank(ALICE);
        (address sub, address resolver,) = registrar.claimWithSubregistry("alice-net");
        vm.startPrank(BOB);
        vm.expectRevert();
        IRegistry(sub).register("evil", BOB, address(0), resolver, 0, uint64(block.timestamp + 30 days));
        vm.expectRevert();
        IResolver(resolver).setText(_dns("laptop", "alice-net"), "pubkey", "evil");
        vm.stopPrank();
    }

    function test_firstComeFirstServed() public {
        vm.prank(ALICE);
        registrar.claim("relay-one");
        vm.prank(BOB);
        vm.expectRevert();
        registrar.claim("relay-one");
        require(nets.findOwner("relay-one") == ALICE, "first claimant keeps it");
    }

    function test_rejectsLabelsThatNeedNormalization() public {
        string[6] memory bad = ["ab", "Alice", "-alice", "alice-", "al ice", "abcdefghijklmnopqrstuvwxyz0123456"];
        for (uint256 i; i < bad.length; ++i) {
            vm.expectRevert(abi.encodeWithSelector(OpenRegistrar.InvalidLabel.selector, bad[i]));
            registrar.claim(bad[i]);
        }
        require(registrar.isValidLabel("a-1"), "short valid label");
    }

    /// @dev DNS wire format of `<a>.<b>.<root>` is unnecessary here: records are keyed by any name.
    function _dns(string memory a, string memory b) internal pure returns (bytes memory) {
        return abi.encodePacked(uint8(bytes(a).length), a, uint8(bytes(b).length), b, uint8(0));
    }

    function _namehash(bytes memory dns) internal pure returns (bytes32 node) {
        return _namehashAt(dns, 0);
    }

    function _namehashAt(bytes memory dns, uint256 offset) internal pure returns (bytes32) {
        uint8 len = uint8(dns[offset]);
        if (len == 0) return bytes32(0);
        bytes memory label = new bytes(len);
        for (uint256 i; i < len; ++i) {
            label[i] = dns[offset + 1 + i];
        }
        return keccak256(abi.encodePacked(_namehashAt(dns, offset + 1 + len), keccak256(label)));
    }
}
