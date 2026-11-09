// SPDX-License-Identifier: MIT
pragma solidity ^0.8.30;

/// @dev The subset of ENSv2 `PermissionedRegistry` this registrar calls.
interface IPermissionedRegistryLike {
    function register(
        string calldata label,
        address owner,
        address registry,
        address resolver,
        uint256 roleBitmap,
        uint64 expiry
    ) external returns (uint256 tokenId);
}

/// @dev ENS `VerifiableFactory`.
interface IVerifiableFactory {
    function deployProxy(address implementation, uint256 salt, bytes calldata data) external returns (address);
}

/// @dev Grant tuple used by ENSv2 `initialize` functions.
struct Grant {
    address account;
    uint256 roleBitmap;
}

interface IUserRegistryInit {
    function initialize(Grant[] calldata grants) external;
}

interface IPermissionedResolverInit {
    function initialize(Grant[] calldata grants, bytes[] calldata calls) external;
}

/// @title OpenRegistrar
/// @notice First come, first served names under one ENSv2 registry, for Bramble tailnets and relays.
/// @dev The registrar holds only `ROLE_REGISTRAR` on its registry. It deploys a resolver (and, for a
///      tailnet, a subregistry) whose roles go to the caller, registers the label to the caller and
///      keeps no role on anything it creates. It cannot change or remove a registered name.
contract OpenRegistrar {
    // ENSv2 registry token roles (RegistryRolesLib at contracts-v2 07e55a0).
    uint256 internal constant ROLE_UNREGISTER = 1 << 12;
    uint256 internal constant ROLE_RENEW = 1 << 16;
    uint256 internal constant ROLE_SET_SUBREGISTRY = 1 << 20;
    uint256 internal constant ROLE_SET_RESOLVER = 1 << 24;
    uint256 internal constant ROLE_CAN_TRANSFER_ADMIN = (1 << 28) << 128;

    /// @notice Roles the claimant receives on the registered name.
    uint256 public constant OWNER_ROLES = ROLE_UNREGISTER | (ROLE_UNREGISTER << 128) | ROLE_RENEW | (ROLE_RENEW << 128)
        | ROLE_SET_SUBREGISTRY | (ROLE_SET_SUBREGISTRY << 128) | ROLE_SET_RESOLVER | (ROLE_SET_RESOLVER << 128)
        | ROLE_CAN_TRANSFER_ADMIN;

    /// @notice Every role on a contract the claimant receives (every nybble's low bit and its admin).
    uint256 public constant ALL_ROLES = 0x1111111111111111111111111111111111111111111111111111111111111111;

    /// @notice Registration term. The owner can renew.
    uint64 public constant TERM = 365 days;

    uint256 public constant MIN_LABEL_LENGTH = 3;
    uint256 public constant MAX_LABEL_LENGTH = 32;

    IPermissionedRegistryLike public immutable registry;
    IVerifiableFactory public immutable factory;
    address public immutable userRegistryImpl;
    address public immutable resolverImpl;

    /// @notice Emitted once per claim. `subregistry` is zero for a relay claim.
    event Claimed(string label, address indexed owner, address subregistry, address resolver, uint256 tokenId);

    error InvalidLabel(string label);
    error ZeroAddress();

    constructor(address registry_, address factory_, address userRegistryImpl_, address resolverImpl_) {
        if (
            registry_ == address(0) || factory_ == address(0) || userRegistryImpl_ == address(0)
                || resolverImpl_ == address(0)
        ) {
            revert ZeroAddress();
        }
        registry = IPermissionedRegistryLike(registry_);
        factory = IVerifiableFactory(factory_);
        userRegistryImpl = userRegistryImpl_;
        resolverImpl = resolverImpl_;
    }

    /// @notice Claims `label` with a new subregistry and resolver, both administered by the caller.
    ///         A tailnet uses this: devices are labels in the subregistry, records live on the resolver.
    function claimWithSubregistry(string calldata label)
        external
        returns (address subregistry, address resolver, uint256 tokenId)
    {
        _checkLabel(label);
        resolver = _deployResolver(label);
        subregistry = factory.deployProxy(
            userRegistryImpl, _salt("subregistry", label), abi.encodeCall(IUserRegistryInit.initialize, (_grants()))
        );
        tokenId = _register(label, subregistry, resolver);
    }

    /// @notice Claims `label` with a new resolver administered by the caller. A relay uses this.
    function claim(string calldata label) external returns (address resolver, uint256 tokenId) {
        _checkLabel(label);
        resolver = _deployResolver(label);
        tokenId = _register(label, address(0), resolver);
    }

    /// @notice Lowercase ASCII letters, digits and inner hyphens, 3 to 32 bytes.
    ///         Such a label is already ENS-normalized, so every client derives the same name.
    function isValidLabel(string calldata label) public pure returns (bool) {
        bytes calldata b = bytes(label);
        if (b.length < MIN_LABEL_LENGTH || b.length > MAX_LABEL_LENGTH) return false;
        if (b[0] == "-" || b[b.length - 1] == "-") return false;
        for (uint256 i; i < b.length; ++i) {
            bytes1 c = b[i];
            bool ok = (c >= "a" && c <= "z") || (c >= "0" && c <= "9") || c == "-";
            if (!ok) return false;
        }
        return true;
    }

    function _checkLabel(string calldata label) internal pure {
        if (!isValidLabel(label)) revert InvalidLabel(label);
    }

    function _deployResolver(string calldata label) internal returns (address) {
        return factory.deployProxy(
            resolverImpl,
            _salt("resolver", label),
            abi.encodeCall(IPermissionedResolverInit.initialize, (_grants(), new bytes[](0)))
        );
    }

    function _register(string calldata label, address subregistry, address resolver)
        internal
        returns (uint256 tokenId)
    {
        tokenId = registry.register(
            label, msg.sender, subregistry, resolver, OWNER_ROLES, uint64(block.timestamp) + TERM
        );
        emit Claimed(label, msg.sender, subregistry, resolver, tokenId);
    }

    function _grants() internal view returns (Grant[] memory grants) {
        grants = new Grant[](1);
        grants[0] = Grant(msg.sender, ALL_ROLES);
    }

    /// @dev The factory mixes in its caller (this contract), so the salt only has to separate claims.
    function _salt(string memory kind, string calldata label) internal view returns (uint256) {
        return uint256(keccak256(abi.encode(kind, address(registry), label, msg.sender, block.number)));
    }
}
